package edge_domain

import (
	agg "control-plane/aggregator"
	rece "control-plane/receive-info"
	"control-plane/routing/routing"
	"control-plane/util"
	"log/slog"
	"math"
	"sort"
	"sync"
)

// getLatencyConfig returns appropriate latency thresholds based on source-destination continent pair
func getLatencyConfig(sourceCont, destCont string) latencyConfig {
	key := sourceCont + "-" + destCont

	// 1. Check for configured continent pairs first
	if config, exists := ContinentPairConfigs[key]; exists {
		return config
	}

	// 2. Inter-continental routes (different continents)
	if sourceCont != destCont {
		return latencyConfig{
			good:    DefaultInterContGood,
			normal:  DefaultInterContNormal,
			warning: DefaultInterContWarning,
		}
	}

	// 3. Intra-continental routes (same continent)
	return latencyConfig{
		good:    DefaultIntraContGood,
		normal:  DefaultIntraContNormal,
		warning: DefaultIntraContWarning,
	}
}

type LyapunovSolver struct {
	edgeAgg map[string]*rece.LastCongestion
	nodeTel map[string]*agg.NodeTelemetry
}

// nodeScore represents a candidate node with its Lyapunov score
type nodeScore struct {
	nodeIp string
	score  float64
}

func NewLyapunovSolver(
	edgeAgg map[string]*rece.LastCongestion,
	nodeTel map[string]*agg.NodeTelemetry,
) *LyapunovSolver {
	return &LyapunovSolver{
		edgeAgg: edgeAgg,
		nodeTel: nodeTel,
	}
}

func (l *LyapunovSolver) Computing(endPoints routing.EndPoints, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {

	logger.Info("Lyapunov last-mile routing start", slog.String("pre", pre), slog.Any("endPoints", endPoints))

	source := endPoints.Source
	continent := source.Continent
	var nodeIps []string

	for _, node := range l.nodeTel {
		//if node.Continent == continent {// TODO: temporary for testing, return all nodes without continent filter
		nodeIps = append(nodeIps, node.PublicIP)
		//}
	}
	if len(nodeIps) <= 0 {
		logger.Warn("no available nodes in continent", slog.String("pre", pre), slog.String("continent", continent))
		nodeIps = []string{util.Config_.Node.IP.Public}
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
			stats = &rece.LastCongestion{AvgRT: 500}
		}

		//score = w * Qk * Δk + V * delay

		cpu := tel.Cpu
		w := 1.0
		//if cpu.LogicalCore > 0 {
		//	w = 1.0 / float64(cpu.LogicalCore)
		//}

		Qk := cpu.Usage
		if Qk < 0 {
			Qk = 0
		}

		delay := stats.AvgRT
		if delay <= 0 {
			delay = 100
		}

		// Get dynamic latency thresholds based on geographic location
		nodeContinent := tel.Continent
		latencyConfig := getLatencyConfig(source.Continent, nodeContinent)

		cpuPenalty := computeCPUPenalty(nodeIp, Qk+math.Abs(cpu.LoadDelta), cpu.LogicalCore)
		delayPenalty := computeDelayPenalty(delay, latencyConfig)
		score := w*cpuPenalty + defaultV*delayPenalty

		candidates = append(candidates, nodeScore{
			nodeIp: nodeIp,
			score:  score,
		})

		logger.Info("Lyapunov last-mile routing score", slog.String("pre", pre),
			slog.String("nodeIp", nodeIp), slog.Float64("score", score),
			slog.Any("w", w), slog.Any("defaultV", defaultV),
			slog.Float64("cpuPenalty", cpuPenalty), slog.Float64("delayPenalty", delayPenalty),
			slog.Float64("Qk", Qk), slog.Float64("deltaK", cpu.LoadDelta),
			slog.Float64("delay", delay))
	}

	if len(candidates) == 0 {
		logger.Warn("no valid nodes after filtering")
		return []routing.PathInfo{}, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score < candidates[j].score
	})

	// Compute softmax probabilities for probabilistic routing
	probabilities := softmaxProbabilities(candidates, softmaxTemp)

	var paths []routing.PathInfo
	for i, item := range candidates {
		weight := 0.0
		if i < len(probabilities) {
			weight = probabilities[i]
		}
		paths = append(paths, routing.PathInfo{
			Hops:   []string{item.nodeIp},
			Rtt:    weight,
			RawRTT: item.score,
		})
	}

	logger.Info("Lyapunov routing completed", slog.String("pre", pre),
		slog.Any("candidate", candidates),
		slog.Any("probabilities", probabilities))

	return paths, nil
}

func (l *LyapunovSolver) GetNodeRT(source routing.EndPoint, nodeIP string, pre string, logger *slog.Logger) *rece.LastCongestion {
	userCity := source.City

	_, nodeExists := l.nodeTel[nodeIP]
	if !nodeExists {
		logger.Warn("node not exists", slog.String("pre", pre), slog.String("nodeIp", nodeIP))
		return nil
	}

	key := userCity + "-" + nodeIP
	val, ok := l.edgeAgg[key]
	if ok {
		return val
	}

	// Default 50ms if no data
	logger.Warn("no latency data, using default 50ms", slog.String("pre", pre),
		slog.String("userCity", userCity), slog.String("nodeIP", nodeIP))
	return &rece.LastCongestion{Count: 1, AvgRT: 50.0}
}

func computeDelayPenalty(rt float64, config latencyConfig) float64 {
	if rt <= config.good {
		return 0.5
	}
	if rt <= config.normal {
		return 1.0
	}
	if rt <= config.warning {
		return 1.5
	}
	return 2.0 + (rt-config.warning)/50.0
}

var (
	penaltyMap = make(map[string]bool)
	penaltyMu  sync.RWMutex
)

func computeCPUPenalty(nodeIP string, Qk float64, logicalCores int) float64 {
	// Get dynamic CPU thresholds based on core count
	config := GetCPUThresholds(logicalCores)

	// TODO: Temporarily disable hysteresis penalty for observation
	// penaltyMu.RLock()
	// inPenalty := penaltyMap[nodeIP]
	// penaltyMu.RUnlock()

	// if Qk >= config.HysteresisUp {
	// 	penaltyMu.Lock()
	// 	penaltyMap[nodeIP] = true
	// 	penaltyMu.Unlock()
	// } else if inPenalty && Qk > config.HysteresisDn {
	// } else if inPenalty && Qk <= config.HysteresisDn {
	// 	penaltyMu.Lock()
	// 	penaltyMap[nodeIP] = false
	// 	penaltyMu.Unlock()
	// }

	// if inPenalty && Qk < config.HysteresisUp {
	// 	Qk = config.HysteresisUp
	// }

	if Qk <= config.Low {
		return 0.5
	}
	if Qk <= config.Mid {
		return 1.0
	}
	if Qk <= config.High {
		return 2.0
	}
	return 4.0 + (Qk-config.High)/10.0
}

// softmaxProbabilities computes softmax probabilities from scores
// Uses negative scores so lower score = higher probability
// P(i) = exp(-S_i / T) / sum(exp(-S_j / T))
func softmaxProbabilities(candidates []nodeScore, temperature float64) []float64 {
	if len(candidates) == 0 {
		return nil
	}

	// Compute exp(-score / temperature) for each candidate
	expScores := make([]float64, len(candidates))
	sumExp := 0.0
	for i, c := range candidates {
		expScores[i] = math.Exp(-c.score / temperature)
		sumExp += expScores[i]
	}

	// Normalize to get probabilities
	probabilities := make([]float64, len(candidates))
	for i := range candidates {
		if sumExp > 0 {
			probabilities[i] = expScores[i] / sumExp
		}
	}

	return probabilities
}
