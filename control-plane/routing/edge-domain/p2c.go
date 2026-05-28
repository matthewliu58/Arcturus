package edge_domain

import (
	agg "control-plane/aggregator"
	"control-plane/routing/routing"
	"control-plane/util"
	"log/slog"
	"math/rand"
)

// P2CRouter implements Power-of-Two-Choices load balancing
// Core idea: randomly pick two nodes, select the one with lower load
// Characteristics:
//   - Extremely lightweight
//   - Decentralized, no global state needed
//   - Excellent scalability
//   - Memoryless, may be sensitive to bursts
type P2CRouter struct {
	nodeTel map[string]*agg.NodeTelemetry
}

func NewP2CRouter(nodeTel map[string]*agg.NodeTelemetry) *P2CRouter {
	return &P2CRouter{nodeTel: nodeTel}
}

// Computing selects nodes using P2C algorithm
// Returns multiple candidate paths sorted by load (lowest first)
func (r *P2CRouter) Computing(endPoints routing.EndPoints, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	logger.Info("P2C last-mile routing start", slog.String("pre", pre), slog.Any("endPoints", endPoints))

	source := endPoints.Source
	continent := source.Continent

	// Collect all nodes in the same continent
	var nodeIps []string
	for _, node := range r.nodeTel {
		//if node.Continent == continent {
		nodeIps = append(nodeIps, node.PublicIP)
		//}
	}

	// Fallback to local node if no continent match
	if len(nodeIps) <= 0 {
		logger.Warn("no available nodes in continent", slog.String("pre", pre), slog.String("continent", continent))
		nodeIps = []string{util.Config_.Node.IP.Public}
	}

	logger.Info("P2C collected nodes", slog.String("pre", pre), slog.Int("nodeCount", len(nodeIps)))

	// If only one node, return directly
	if len(nodeIps) == 1 {
		tel, ok := r.nodeTel[nodeIps[0]]
		if !ok {
			logger.Warn("missing telemetry for single node", slog.String("pre", pre), slog.String("nodeIp", nodeIps[0]))
			return []routing.PathInfo{}, nil
		}
		load := tel.Cpu.Usage
		if load < 0 {
			load = 100
		}
		logger.Info("P2C single node available", slog.String("pre", pre), slog.String("nodeIp", nodeIps[0]), slog.Float64("load", load))
		return []routing.PathInfo{{Hops: []string{nodeIps[0]}, Rtt: load}}, nil
	}

	// P2C: Randomly pick two nodes and compare their loads
	type nodeLoad struct {
		nodeIp string
		load   float64
	}

	var candidates []nodeLoad

	// If 4 or fewer nodes, evaluate all directly
	if len(nodeIps) <= 4 {
		for _, nodeIp := range nodeIps {
			tel, telOk := r.nodeTel[nodeIp]
			if !telOk {
				logger.Warn("P2C skip node: missing telemetry", slog.String("pre", pre), slog.String("nodeIp", nodeIp))
				continue
			}
			load := tel.Cpu.Usage
			if load < 0 {
				load = 100
			}
			candidates = append(candidates, nodeLoad{nodeIp: nodeIp, load: load})
		}
	} else {
		// P2C: Randomly sample two nodes and pick the better one
		// Run multiple rounds to get multiple candidates
		for round := 0; round < len(nodeIps); round++ {
			// Randomly pick two distinct nodes
			a := rand.Intn(len(nodeIps))
			b := rand.Intn(len(nodeIps))
			for b == a {
				b = rand.Intn(len(nodeIps))
			}

			nodeA := nodeIps[a]
			nodeB := nodeIps[b]

			telA, okA := r.nodeTel[nodeA]
			telB, okB := r.nodeTel[nodeB]

			loadA := 100.0
			loadB := 100.0

			if okA {
				loadA = telA.Cpu.Usage
				if loadA < 0 {
					loadA = 100
				}
			}
			if okB {
				loadB = telB.Cpu.Usage
				if loadB < 0 {
					loadB = 100
				}
			}

			// Select the one with lower load
			var chosen nodeLoad
			if loadA <= loadB {
				chosen = nodeLoad{nodeIp: nodeA, load: loadA}
			} else {
				chosen = nodeLoad{nodeIp: nodeB, load: loadB}
			}

			// Check if already in candidates
			exists := false
			for _, c := range candidates {
				if c.nodeIp == chosen.nodeIp {
					exists = true
					break
				}
			}

			if !exists {
				candidates = append(candidates, chosen)
				logger.Debug("P2C selected node",
					slog.String("pre", pre),
					slog.String("nodeA", nodeA), slog.Float64("loadA", loadA),
					slog.String("nodeB", nodeB), slog.Float64("loadB", loadB),
					slog.String("chosen", chosen.nodeIp), slog.Float64("chosenLoad", chosen.load))
			}

			// Stop if we have enough candidates
			if len(candidates) >= 3 {
				break
			}
		}
	}

	if len(candidates) == 0 {
		logger.Warn("P2C no valid nodes after selection")
		return []routing.PathInfo{}, nil
	}

	// Sort by load (ascending)
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].load < candidates[i].load {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Convert to PathInfo
	var paths []routing.PathInfo
	for _, c := range candidates {
		paths = append(paths, routing.PathInfo{
			Hops: []string{c.nodeIp},
			Rtt:  c.load,
		})
	}

	logger.Info("P2C routing completed",
		slog.String("pre", pre),
		slog.Int("candidateCount", len(paths)),
		slog.Any("paths", paths))

	return paths, nil
}
