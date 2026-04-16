package last_mile

import (
	agg "control-plane/aggregator"
	"control-plane/routing/routing"
	"control-plane/util"
	"log/slog"
	"sort"
)

type CPUOnlyRouter struct {
	nodeTel map[string]*agg.NodeTelemetry
}

func NewCPUOnlyRouter(nodeTel map[string]*agg.NodeTelemetry) *CPUOnlyRouter {
	return &CPUOnlyRouter{nodeTel: nodeTel}
}

func (r *CPUOnlyRouter) Computing(endPoints routing.EndPoints, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	logger.Info("CPUOnly last-mile routing start", slog.String("pre", pre), slog.Any("endPoints", endPoints))

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

		Qk := tel.Cpu.Usage
		if Qk < 0 {
			Qk = 100
		}

		score := Qk
		candidates = append(candidates, nodeScore{nodeIp: nodeIp, score: score})

		logger.Info("CPUOnly routing score",
			slog.String("pre", pre), slog.String("nodeIp", nodeIp), slog.Float64("score", score), slog.Float64("cpu", Qk))
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

	logger.Info("CPUOnly routing completed", slog.String("pre", pre), slog.Any("paths", paths))
	return paths, nil
}
