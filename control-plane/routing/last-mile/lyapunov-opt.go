package last_mile

import (
	agg "control-plane/info-agg"
	rece "control-plane/receive-info"
	"control-plane/routing/routing"
	"log/slog"
	"math"
	"sort"
	"sync"
)

const (
	// 延迟阶梯（ms）
	LatencyGood    = 50.0
	LatencyNormal  = 100.0
	LatencyWarning = 150.0

	// CPU 负载阶梯（%）
	CPULow  = 40.0
	CPUMid  = 60.0 // 安全线
	CPUHigh = 80.0

	// 均衡系数
	defaultV = 0.5
)

// LyapunovSolver 李雅普诺夫漂移调度核心
type LyapunovSolver struct {
	globalStats  *agg.GlobalStats // 直接持有 stats，才能获取 nodeLast + edgeAgg
	nodeTel      map[string]*agg.Telemetry
	nodeLocation map[string][]string
}

// NewLyapunovSolver 创建实例
func NewLyapunovSolver(
	globalStats *agg.GlobalStats,
	nodeTel map[string]*agg.Telemetry,
	nodeLocation map[string][]string,
) *LyapunovSolver {
	return &LyapunovSolver{
		globalStats:  globalStats,
		nodeTel:      nodeTel,
		nodeLocation: nodeLocation,
	}
}

// Computing 执行李雅普诺夫最优路由计算
func (l *LyapunovSolver) Computing(endPoints routing.EndPoints, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {

	logger.Info("Lyapunov last-mile routing start", slog.String("pre", pre), slog.Any("endPoints", endPoints))

	source := endPoints.Source
	continent := source.Continent
	nodeIps := l.nodeLocation[continent]

	if len(nodeIps) == 0 {
		logger.Warn("no available nodes in continent", slog.String("pre", pre), slog.String("continent", continent))
		return []routing.PathInfo{}, nil
	}

	type nodeScore struct {
		nodeIp string
		score  float64 // 分数越小，越优
		valid  bool
	}

	var candidates []nodeScore

	for _, nodeIp := range nodeIps {
		// 1. 获取节点遥测数据
		tel, telOk := l.nodeTel[nodeIp]
		if !telOk {
			logger.Warn("skip node: missing telemetry", slog.String("pre", pre), slog.String("nodeIp", nodeIp))
			continue
		}

		// 2. 获取 RT
		stats := l.GetNodeRT(source, nodeIp, pre, logger)
		if stats == nil || stats.Count == 0 {
			logger.Warn("skip node: no rt stats", slog.String("pre", pre), slog.String("nodeIp", nodeIp))
			continue
		}

		// 公式：score = w * Qk * Δk + V * delay

		// w = 1 / 逻辑核心数（异构公平加权）
		cpu := tel.Cpu
		w := 1.0
		if cpu.LogicalCore > 0 {
			w = 1.0 / float64(cpu.LogicalCore)
		}

		// Qk = CPU 使用率（积压）
		Qk := cpu.Usage
		if Qk < 0 {
			Qk = 0
		}

		// Δk = 瞬时负载变化量（冲击）
		deltaK := cpu.LoadDelta
		if deltaK < 0 {
			deltaK = 0
		}

		// delay = 延迟（用户体验惩罚项）
		delay := stats.AvgRT
		if delay <= 0 {
			delay = 100 // 兜底值，避免 0 导致异常
		}

		// 计算最终 score
		cpuPenalty := computeCPUPenalty(nodeIp, Qk+math.Abs(deltaK))
		delayPenalty := computeDelayPenalty(delay)
		score := w*cpuPenalty + defaultV*delayPenalty

		candidates = append(candidates, nodeScore{
			nodeIp: nodeIp,
			score:  score,
		})
	}

	if len(candidates) == 0 {
		logger.Warn("no valid nodes after filtering")
		return []routing.PathInfo{}, nil
	}

	// 排序：score 越小，优先级越高
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score < candidates[j].score
	})

	// 构造 PathInfo 返回
	var paths []routing.PathInfo
	for _, item := range candidates {
		paths = append(paths, routing.PathInfo{
			Hops: []string{item.nodeIp},
			Rtt:  item.score,
		})
	}

	logger.Info("Lyapunov routing completed", slog.String("pre", pre), slog.Any("paths", paths))

	return paths, nil
}

func (l *LyapunovSolver) GetNodeRT(source routing.EndPoint, nodeIP string, pre string, logger *slog.Logger) *rece.LastStatsVal {
	userContinent := source.Continent
	userCountry := source.Country
	userCity := source.City

	// 1. 获取节点原始信息
	node, nodeExists := l.nodeTel[nodeIP]
	if !nodeExists {
		logger.Warn("node not exists", slog.String("pre", pre), slog.String("nodeIp", nodeIP))
		return nil
	}

	// 2. 按精确优先级构造 key
	keys := []string{
		userCity + "-" + nodeIP,
		userCity + "-" + node.City,
		userCountry + "-" + node.Country,
		userContinent + "-" + node.Continent,
		userContinent + "-general",
	}

	// 3. 依次查找
	for _, key := range keys {
		val := l.globalStats.GetAggValue(key)
		if val.Count > 0 {
			return val
		}
	}

	return &rece.LastStatsVal{}
}

// getAggFallback 兜底
func (l *LyapunovSolver) getAggFallback(cont, serverKey string) *rece.LastStatsVal {
	key := cont + "-" + serverKey
	return l.globalStats.GetAggValue(key)
}

func computeDelayPenalty(rt float64) float64 {
	if rt <= LatencyGood {
		return 0.5
	}
	if rt <= LatencyNormal {
		return 1.0
	}
	if rt <= LatencyWarning {
		return 1.5
	}
	return 2.0 + (rt-LatencyWarning)/50.0
}

var (
	penaltyMap = make(map[string]bool)
	penaltyMu  sync.RWMutex
)

// 内部自动加入 60% → 45% 滞回防震荡
func computeCPUPenalty(nodeIP string, Qk float64) float64 {
	const (
		CPU_HIGH = 60.0 // 你原来的安全线
		CPU_LOW  = 45.0 // 恢复线
	)

	// 1. 查当前是否在惩罚中
	penaltyMu.RLock()
	inPenalty := penaltyMap[nodeIP]
	penaltyMu.RUnlock()

	if Qk >= CPU_HIGH {
		// 超过高线 → 标记惩罚
		penaltyMu.Lock()
		penaltyMap[nodeIP] = true
		penaltyMu.Unlock()
	} else if inPenalty && Qk > CPU_LOW {
		// 惩罚中 && 没回落到45以下 → 继续惩罚
	} else if inPenalty && Qk <= CPU_LOW {
		// 回落到安全线 → 解除惩罚
		penaltyMu.Lock()
		penaltyMap[nodeIP] = false
		penaltyMu.Unlock()
	}

	if inPenalty && Qk < CPU_HIGH {
		Qk = CPU_HIGH
	}

	if Qk <= CPULow {
		return 0.5
	}
	if Qk <= CPUMid {
		return 1.0
	}
	if Qk <= CPUHigh {
		return 2.0
	}
	return 4.0 + (Qk-CPUHigh)/10.0
}
