package last_mile

import (
	agg "control-plane/aggregator"
	rece "control-plane/receive-info"
	"control-plane/routing/routing"
	"control-plane/util"
	"log/slog"
	"sort"
)

type JointRouter struct {
	edgeAgg       map[string]*rece.LastCongestion
	nodeTel       map[string]*agg.NodeTelemetry
	cpuWeight     float64
	latencyWeight float64
}

func NewJointRouter(
	edgeAgg map[string]*rece.LastCongestion,
	nodeTel map[string]*agg.NodeTelemetry,
) *JointRouter {
	return &JointRouter{
		edgeAgg:       edgeAgg,
		nodeTel:       nodeTel,
		cpuWeight:     0.5, // Default CPU weight
		latencyWeight: 0.5, // Default latency weight
	}
}

// SetWeights sets the weights for CPU and latency
func (r *JointRouter) SetWeights(cpuWeight, latencyWeight float64) {
	if cpuWeight > 0 && latencyWeight > 0 {
		total := cpuWeight + latencyWeight
		r.cpuWeight = cpuWeight / total
		r.latencyWeight = latencyWeight / total
	} else if cpuWeight == 0 && latencyWeight > 0 {
		r.cpuWeight = 0
		r.latencyWeight = 1
	} else if latencyWeight == 0 && cpuWeight > 0 {
		r.cpuWeight = 1
		r.latencyWeight = 0
	}
	// If both are 0 or any is negative, keep current values (default 0.5, 0.5)
}

func (r *JointRouter) Computing(endPoints routing.EndPoints, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	logger.Info("JointCPU-Latency last-mile routing start", slog.String("pre", pre), slog.Any("endPoints", endPoints))

	source := endPoints.Source
	continent := source.Continent
	var nodeIps []string

	for _, node := range r.nodeTel {
		if node.Continent == continent {
			nodeIps = append(nodeIps, node.PublicIP)
		}
	}

	if len(nodeIps) <= 0 {
		logger.Warn("no available nodes in continent", slog.String("pre", pre), slog.String("continent", continent))
		nodeIps = []string{util.Config_.Node.IP.Public}
	}

	type nodeScore struct {
		nodeIp string
		score  float64
	}

	var candidates []nodeScore

	for _, nodeIp := range nodeIps {
		tel, telOk := r.nodeTel[nodeIp]
		if !telOk {
			logger.Warn("skip node: missing telemetry", slog.String("pre", pre), slog.String("nodeIp", nodeIp))
			continue
		}

		stats := r.GetNodeRT(source, nodeIp, pre, logger)
		if stats == nil || stats.Count == 0 {
			logger.Warn("skip node: no rt stats", slog.String("pre", pre), slog.String("nodeIp", nodeIp))
			stats = &rece.LastCongestion{AvgRT: 500}
		}

		delay := stats.AvgRT
		if delay <= 0 {
			delay = 100
		}

		Qk := tel.Cpu.Usage
		if Qk < 0 {
			Qk = 100
		}

		score := r.cpuWeight*Qk + r.latencyWeight*delay
		candidates = append(candidates, nodeScore{nodeIp: nodeIp, score: score})

		logger.Info("Joint routing score",
			slog.String("pre", pre), slog.String("nodeIp", nodeIp), slog.Float64("score", score),
			slog.Float64("cpu", Qk), slog.Float64("delay", delay),
			slog.Float64("cpuWeight", r.cpuWeight), slog.Float64("latencyWeight", r.latencyWeight))
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
		paths = append(paths, routing.PathInfo{Hops: []string{item.nodeIp}, Rtt: item.score})
	}

	logger.Info("Joint routing completed", slog.String("pre", pre), slog.Any("paths", paths))
	return paths, nil
}

func (r *JointRouter) GetNodeRT(source routing.EndPoint, nodeIP string, pre string, logger *slog.Logger) *rece.LastCongestion {
	userContinent := source.Continent
	userCountry := source.Country
	userCity := source.City

	node, nodeExists := r.nodeTel[nodeIP]
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
		val, ok := r.edgeAgg[key]
		if ok {
			return val
		}
	}

	return &rece.LastCongestion{}
}
