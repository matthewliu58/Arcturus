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

		cpuPenalty := computeCPUPenalty(Qk+math.Abs(cpu.LoadDelta), cpu.LogicalCore)
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
			slog.Float64("queue_backlog", Qk), slog.Float64("queue_delta", math.Abs(cpu.LoadDelta)),
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

// computeDelayPenalty returns an exponential delay penalty:
//
//	delayPenalty = exp(sensitivityDelay * (rt / config.good - 1))
//
// At rt=good, penalty=1.0. Below: sub-linear decay. Above: exponential growth.
// Example with good=20ms, β=0.20: 10ms→0.90, 20ms→1.0, 40ms→1.22, 60ms→1.49, 80ms→1.82
func computeDelayPenalty(rt float64, config latencyConfig) float64 {
	if config.good <= 0 {
		return math.Exp(sensitivityDelay * (rt/50.0 - 1)) // fallback
	}
	return math.Exp(sensitivityDelay * (rt/config.good - 1))
}

var (
	penaltyMap = make(map[string]bool)
	penaltyMu  sync.RWMutex
)

// computeCPUPenalty returns an exponential CPU penalty:
//
//	cpuPenalty = exp(config.SensitivityCPU * (Qk / config.Mid - 1))
//
// At Qk=Mid, penalty=1.0. Below: sub-linear decay. Above: exponential growth.
//
// Examples:
//   2-core (α=2.0, Mid=60%): 20%→0.26, 40%→0.51, 60%→1.0, 80%→1.95, 100%→3.79
//   4-core (α=3.0, Mid=80%): 40%→0.22, 60%→0.47, 80%→1.0, 100%→2.12, 110%→3.22
func computeCPUPenalty(Qk float64, logicalCores int) float64 {
	config := GetCPUThresholds(logicalCores)

	alpha := config.SensitivityCPU
	if alpha <= 0 {
		alpha = sensitivityCPU // fallback to global default
	}
	if config.Mid <= 0 {
		return math.Exp(alpha * (Qk/60.0 - 1)) // fallback
	}
	return math.Exp(alpha * (Qk/config.Mid - 1))
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
