package last_mile

import (
	agg "control-plane/aggregator"
	rece "control-plane/receive-info"
	"control-plane/routing/routing"
	"control-plane/util"
	"log/slog"
	"sort"
)

type LatencyOnlyRouter struct {
	edgeAgg map[string]*rece.LastCongestion
	nodeTel map[string]*agg.NodeTelemetry
}

func NewLatencyOnlyRouter(
	edgeAgg map[string]*rece.LastCongestion,
	nodeTel map[string]*agg.NodeTelemetry,
) *LatencyOnlyRouter {
	return &LatencyOnlyRouter{
		edgeAgg: edgeAgg,
		nodeTel: nodeTel,
	}
}

func (r *LatencyOnlyRouter) Computing(endPoints routing.EndPoints, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	logger.Info("LatencyOnly last-mile routing start", slog.String("pre", pre), slog.Any("endPoints", endPoints))

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
		_, telOk := r.nodeTel[nodeIp]
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

		score := delay
		candidates = append(candidates, nodeScore{nodeIp: nodeIp, score: score})

		logger.Info("LatencyOnly routing score",
			slog.String("pre", pre), slog.String("nodeIp", nodeIp), slog.Float64("score", score), slog.Float64("delay", delay))
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

	logger.Info("LatencyOnly routing completed", slog.String("pre", pre), slog.Any("paths", paths))
	return paths, nil
}

func (r *LatencyOnlyRouter) GetNodeRT(source routing.EndPoint, nodeIP string, pre string, logger *slog.Logger) *rece.LastCongestion {
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
