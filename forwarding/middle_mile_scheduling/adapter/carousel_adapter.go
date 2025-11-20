package adapter

import (
	"fmt"
	"forwarding/middle_mile_scheduling/carousel_greedy/algorithm"
	"forwarding/middle_mile_scheduling/carousel_greedy/graph"
	"forwarding/middle_mile_scheduling/common"

	log "github.com/sirupsen/logrus"
)

// All edges have uniform latency=1 to isolate latency from path selection
type CarouselAdapter struct {
	// No parameters needed - uses fixed configuration for weight-driven mode
}

// NewCarouselAdapter creates a new Carousel adapter
func NewCarouselAdapter() *CarouselAdapter {
	log.Infof("CarouselAdapter initialized (latency-only mode, weight-based)")
	return &CarouselAdapter{}
}

// ComputePaths implements common.PathCalculator.ComputePaths for Carousel-Greedy algorithm
func (ca *CarouselAdapter) ComputePaths(
	network *common.Network,
	source int,
	dest int,
	params map[string]interface{},
	ipToIndex map[string]int,
	indexToIP map[int]string,
) []common.PathWithIP {

	// Validate inputs
	if network == nil || network.Nodes == nil || len(network.Nodes) == 0 {
		log.Warnf("CarouselAdapter: invalid network provided")
		return []common.PathWithIP{}
	}

	if source < 0 || dest < 0 || source >= len(network.Nodes) || dest >= len(network.Nodes) {
		log.Warnf("CarouselAdapter: invalid source or dest index: source=%d, dest=%d, nodes=%d",
			source, dest, len(network.Nodes))
		return []common.PathWithIP{}
	}

	// Convert public Network to private graph structure used by carousel_greedy
	carouselGraph := ca.convertNetworkToGraph(network, source, dest)
	if carouselGraph == nil {
		log.Warnf("CarouselAdapter: failed to convert network to graph")
		return []common.PathWithIP{}
	}

	// Call the actual Carousel-Greedy algorithm with fixed parameters
	// thetaA: allow paths of any length
	// thetaL: all edges have latency=1, so this has no effect
	// maxEdgeUsage: each edge can be used at most once (for disjoint paths)
	log.Infof("CarouselAdapter.ComputePaths: starting carousel-greedy computation with source=%d, dest=%d, network has %d nodes",
		source, dest, len(network.Nodes))

	carouselPaths := algorithm.GreedyMFPC(carouselGraph, float64(len(network.Nodes)), 10, 1)

	log.Infof("CarouselAdapter.ComputePaths: carousel-greedy algorithm returned %d raw paths", len(carouselPaths))

	// Convert carousel paths back to PathWithIP format
	result := ca.convertCarouselPathsToPathWithIP(carouselPaths, indexToIP)

	log.Infof("CarouselAdapter.ComputePaths: completed, converted to %d PathWithIP objects for source=%d, dest=%d",
		len(result), source, dest)
	return result
}

// convertNetworkToGraph converts the public Network structure to carousel's private Graph structure
func (ca *CarouselAdapter) convertNetworkToGraph(network *common.Network, source, dest int) *graph.Graph {
	if network == nil || len(network.Nodes) == 0 {
		return nil
	}

	numNodes := len(network.Nodes)
	g := graph.NewGraph(numNodes, source, dest)

	// Add edges from the network Links matrix
	// Links[i][j] contains the latency between nodes i and j
	// -1 means no link exists
	if network.Links == nil || len(network.Links) == 0 {
		log.Warnf("CarouselAdapter: network has no links")
		return g
	}

	for i := 0; i < numNodes && i < len(network.Links); i++ {
		if network.Links[i] == nil {
			continue
		}
		for j := 0; j < numNodes && j < len(network.Links[i]); j++ {
			latency := network.Links[i][j]
			if latency >= 0 { // Only add valid links (latency >= 0)
				// Weight-driven mode: use weight = 1/latency as capacity
				// Higher latency → lower weight → lower capacity → less attractive
				// Lower latency → higher weight → higher capacity → more attractive
				var weight float64
				if latency == 0 {
					weight = 1.0 // Handle zero latency: assign very high weight
				} else {
					weight = 1.0 / float64(latency) // weight = 1/latency (inverse latency)
				}
				// Use weight as capacity, keep latency as 1 for uniform DFS cost
				g.AddEdge(i, j, weight, 1.0) // capacity=weight, latency=1 (uniform)
			}
		}
	}

	log.Debugf("CarouselAdapter: converted network with %d nodes to carousel graph", numNodes)
	return g
}

// convertCarouselPathsToPathWithIP converts carousel's internal Path format to PathWithIP format
// Calculates weight based on flow (which is driven by capacity/weight in weight-driven mode)
func (ca *CarouselAdapter) convertCarouselPathsToPathWithIP(
	carouselPaths []*graph.Path,
	indexToIP map[int]string,
) []common.PathWithIP {
	result := make([]common.PathWithIP, 0, len(carouselPaths))

	// Step 1: Calculate total flow to normalize weights
	var totalFlow float64
	for _, p := range carouselPaths {
		totalFlow += p.Flow
	}

	// If no flow found, use number of paths as fallback
	// This ensures weights are normalized even with edge cases
	if totalFlow == 0 {
		totalFlow = float64(len(carouselPaths))
	}

	log.Debugf("CarouselAdapter: total flow = %.2f, paths count = %d", totalFlow, len(carouselPaths))

	// Step 2: Convert paths and calculate normalized weights
	for _, carouselPath := range carouselPaths {
		if carouselPath == nil || len(carouselPath.Nodes) == 0 {
			continue
		}

		// Convert node indices to IP addresses
		ipList := make([]string, len(carouselPath.Nodes))
		for i, nodeIdx := range carouselPath.Nodes {
			if ip, ok := indexToIP[nodeIdx]; ok {
				ipList[i] = ip
			} else {
				log.Warnf("CarouselAdapter: node index %d not found in indexToIP mapping", nodeIdx)
				// Still add the unknown IP to maintain path structure
				ipList[i] = fmt.Sprintf("unknown_%d", nodeIdx)
			}
		}

		// Calculate weight based on flow (normalized to [0-100])
		// Higher flow = higher weight = more traffic
		weight := int((carouselPath.Flow / totalFlow) * 100)
		if weight == 0 && carouselPath.Flow > 0 {
			weight = 1 // Minimum weight of 1 to avoid zero weights
		}

		pathWithIP := common.PathWithIP{
			IPList:  ipList,
			Latency: int(carouselPath.Latency), // Now all equal to 1 in weight-driven mode
			Weight:  weight,                    // Normalized weight [0-100]
		}
		result = append(result, pathWithIP)

		log.Debugf("CarouselAdapter: converted path with %d hops, flow=%.2f, weight=%d",
			len(carouselPath.Nodes), carouselPath.Flow, weight)
	}

	return result
}
