package edge_domain

import (
	agg "control-plane/aggregator"
	rece "control-plane/receive-info"
	"control-plane/routing/routing"
	"control-plane/util"
	"log/slog"
)

// P2CRouter implements Power-of-Two-Choices inspired load balancing
// Core idea: distribute traffic across ALL nodes proportionally based on inverse CPU load
// (lower load = higher weight, i.e., weight = 1/CPU)
// Characteristics:
//   - Proportional distribution across ALL available nodes
//   - Lower CPU gets higher weight (1/CPU)
//   - Probabilistic routing based on weights
//   - Simple and intuitive
type P2CRouter struct {
	nodeTel map[string]*agg.NodeTelemetry
	edgeAgg map[string]*rece.LastCongestion
}

func NewP2CRouter(nodeTel map[string]*agg.NodeTelemetry, edgeAgg map[string]*rece.LastCongestion) *P2CRouter {
	return &P2CRouter{nodeTel: nodeTel, edgeAgg: edgeAgg}
}

// Computing selects nodes using inverse-CPU proportional distribution across ALL nodes.
// Weight = 1/CPU (lower CPU = higher weight), normalized across all nodes.
// The Rtt field carries the proportional weight for downstream probabilistic routing.
func (r *P2CRouter) Computing(endPoints routing.EndPoints, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	logger.Info("P2C last-mile routing start", slog.String("pre", pre), slog.Any("endPoints", endPoints))

	source := endPoints.Source
	continent := source.Continent

	// Collect all nodes
	var nodeIps []string
	for _, node := range r.nodeTel {
		nodeIps = append(nodeIps, node.PublicIP)
	}

	// Fallback to local node if no nodes available
	if len(nodeIps) <= 0 {
		logger.Warn("no available nodes", slog.String("pre", pre), slog.String("continent", continent))
		nodeIps = []string{util.Config_.Node.IP.Public}
	}

	logger.Info("P2C collected nodes", slog.String("pre", pre), slog.Int("nodeCount", len(nodeIps)))

	// Calculate inverse-CPU weights for all nodes
	type nodeWeight struct {
		nodeIP string
		load   float64
		weight float64
	}

	var weights []nodeWeight
	var totalWeight float64

	for _, nodeIP := range nodeIps {
		load := 100.0 // Default max load
		if tel, ok := r.nodeTel[nodeIP]; ok {
			load = tel.Cpu.Usage
			if load < 0 || load == 0 {
				load = 100
			}
		}

		// Weight = 1/CPU (lower CPU = higher weight)
		weight := 1.0 / load
		weights = append(weights, nodeWeight{
			nodeIP: nodeIP,
			load:   load,
			weight: weight,
		})
		totalWeight += weight
	}

	// Normalize weights to probabilities
	var paths []routing.PathInfo
	for _, w := range weights {
		prob := w.weight / totalWeight
		paths = append(paths, routing.PathInfo{
			Hops: []string{w.nodeIP},
			Rtt:  prob,
		})
		logger.Info("P2C node weight",
			slog.String("pre", pre),
			slog.String("nodeIP", w.nodeIP),
			slog.Float64("load", w.load),
			slog.Float64("latency", r.getNodeLatency(source, w.nodeIP)),
			slog.Float64("weight", w.weight),
			slog.Float64("prob", prob))
	}

	logger.Info("P2C routing completed",
		slog.String("pre", pre),
		slog.Int("candidateCount", len(paths)),
		slog.Any("paths", paths))

	return paths, nil
}

// getNodeLatency returns the user-to-node latency from edgeAgg.
func (r *P2CRouter) getNodeLatency(source routing.EndPoint, nodeIP string) float64 {
	key := source.City + "-" + nodeIP
	if cong, ok := r.edgeAgg[key]; ok && cong.Count > 0 {
		return cong.AvgRT
	}
	return 50.0 // default
}
