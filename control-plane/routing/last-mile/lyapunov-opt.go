package last_mile

import (
	agg "control-plane/aggregator"
	rece "control-plane/receive-info"
	"control-plane/routing/routing"
	"log/slog"
	"math"
	"sort"
	"sync"
)

const (
	LatencyGood    = 50.0
	LatencyNormal  = 100.0
	LatencyWarning = 150.0
	CPULow         = 40.0
	CPUMid         = 60.0
	CPUHigh        = 80.0
	defaultV       = 0.5
)

type LyapunovSolver struct {
	edgeAgg      map[string]*rece.LastStatsVal
	nodeTel      map[string]*agg.Telemetry
	nodeLocation map[string][]string
}

func NewLyapunovSolver(
	edgeAgg map[string]*rece.LastStatsVal,
	nodeTel map[string]*agg.Telemetry,
	nodeLocation map[string][]string,
) *LyapunovSolver {
	return &LyapunovSolver{
		edgeAgg:      edgeAgg,
		nodeTel:      nodeTel,
		nodeLocation: nodeLocation,
	}
}

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
		score  float64
		valid  bool
	}

	var candidates []nodeScore

	for _, nodeIp := range nodeIps {

		tel, telOk := l.nodeTel[nodeIp]
		if !telOk {
			logger.Warn("skip node: missing telemetry", slog.String("pre", pre), slog.String("nodeIp", nodeIp))
			continue
		}

		stats := l.GetNodeRT(source, nodeIp, pre, logger)
		if stats == nil || stats.Count == 0 {
			logger.Warn("skip node: no rt stats", slog.String("pre", pre), slog.String("nodeIp", nodeIp))
			continue
		}

		//score = w * Qk * Δk + V * delay

		cpu := tel.Cpu
		w := 1.0
		if cpu.LogicalCore > 0 {
			w = 1.0 / float64(cpu.LogicalCore)
		}

		Qk := cpu.Usage
		if Qk < 0 {
			Qk = 0
		}

		deltaK := cpu.LoadDelta
		if deltaK < 0 {
			deltaK = 0
		}

		delay := stats.AvgRT
		if delay <= 0 {
			delay = 100
		}

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

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score < candidates[j].score
	})

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

	node, nodeExists := l.nodeTel[nodeIP]
	if !nodeExists {
		logger.Warn("node not exists", slog.String("pre", pre), slog.String("nodeIp", nodeIP))
		return nil
	}

	keys := []string{
		userCity + "-" + nodeIP,
		userCity + "-" + node.City,
		userCountry + "-" + node.Country,
		userContinent + "-" + node.Continent,
		userContinent + "-general",
	}

	for _, key := range keys {
		val, ok := l.edgeAgg[key]
		if ok {
			return val
		}
	}

	return &rece.LastStatsVal{}
}

func (l *LyapunovSolver) getAggFallback(cont, serverKey string) *rece.LastStatsVal {
	key := cont + "-" + serverKey
	return l.edgeAgg[key]
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

func computeCPUPenalty(nodeIP string, Qk float64) float64 {
	const (
		CPU_HIGH = 60.0
		CPU_LOW  = 45.0
	)

	penaltyMu.RLock()
	inPenalty := penaltyMap[nodeIP]
	penaltyMu.RUnlock()

	if Qk >= CPU_HIGH {
		penaltyMu.Lock()
		penaltyMap[nodeIP] = true
		penaltyMu.Unlock()
	} else if inPenalty && Qk > CPU_LOW {
	} else if inPenalty && Qk <= CPU_LOW {
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
