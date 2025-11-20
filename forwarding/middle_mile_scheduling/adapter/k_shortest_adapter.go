package adapter

import (
	"fmt"

	"forwarding/middle_mile_scheduling/common"
	"forwarding/middle_mile_scheduling/k_shortest"

	log "github.com/sirupsen/logrus"
)

// KShortestAdapter implements the common.PathCalculator interface for K-Shortest algorithm
type KShortestAdapter struct {
	k int // number of shortest paths to compute
}

// NewKShortestAdapter creates a new K-Shortest adapter with the specified k value
func NewKShortestAdapter(k int) *KShortestAdapter {
	return &KShortestAdapter{k: k}
}

// ComputePaths implements common.PathCalculator.ComputePaths for K-Shortest algorithm
// It converts between common data structures and k_shortest-specific structures
// Parameters: params should contain {"k": <number>} for k value
func (ka *KShortestAdapter) ComputePaths(
	network *common.Network,
	source int,
	dest int,
	params map[string]interface{},
	ipToIndex map[string]int,
	indexToIP map[int]string,
) []common.PathWithIP {
	// Extract k from params, fallback to ka.k if not provided
	k := ka.k
	if kVal, exists := params["k"]; exists {
		if kInt, ok := kVal.(int); ok {
			k = kInt
		}
	}

	log.Infof("KShortestAdapter.ComputePaths: starting k-shortest computation with k=%d, source=%d, dest=%d, network has %d nodes",
		k, source, dest, len(network.Nodes))

	// Convert common.Network to k_shortest package types for algorithm execution
	ksNetwork := convertNetwork(network)

	// Compute k-shortest paths using the k_shortest algorithm
	paths := k_shortest.KShortest(ksNetwork, k_shortest.Flow{
		Source:      source,
		Destination: dest,
	}, k, 3, 2)

	log.Infof("KShortestAdapter.ComputePaths: k_shortest algorithm returned %d raw paths", len(paths))

	// Convert paths to common.PathWithIP format
	result := make([]common.PathWithIP, 0, len(paths))
	var totalLatency int
	for _, p := range paths {
		totalLatency += p.Latency
	}

	if len(paths) == 0 {
		log.Warnf("KShortestAdapter.ComputePaths: k_shortest returned no paths for source=%d, dest=%d", source, dest)
		return result
	}

	for idx, path := range paths {
		ipList := make([]string, len(path.Nodes))
		for i, nodeIdx := range path.Nodes {
			if ip, exists := indexToIP[nodeIdx]; exists {
				ipList[i] = ip
			} else {
				ipList[i] = fmt.Sprintf("node-%d", nodeIdx)
			}
		}

		var weight int
		if path.Latency == 0 {
			weight = 100
		} else if totalLatency == 0 {
			weight = 1
		} else {
			weight = totalLatency / path.Latency
		}

		pathWithIP := common.PathWithIP{
			IPList:  ipList,
			Latency: path.Latency,
			Weight:  weight,
		}
		result = append(result, pathWithIP)

		log.Infof("KShortestAdapter.ComputePaths: path[%d] has %d hops, latency=%d, weight=%d, nodes=%v",
			idx, len(ipList), path.Latency, weight, ipList)
	}

	log.Infof("KShortestAdapter.ComputePaths: completed, produced %d paths (totalLatency=%d)",
		len(result), totalLatency)

	return result
}

// convertNetwork converts common.Network to k_shortest package Network type for algorithm execution
func convertNetwork(network *common.Network) k_shortest.Network {
	return k_shortest.Network{
		Nodes: convertNodes(network.Nodes),
		Links: network.Links,
	}
}

// convertNodes converts common.Node slice to k_shortest.Node slice
func convertNodes(nodes []common.Node) []k_shortest.Node {
	result := make([]k_shortest.Node, len(nodes))
	// Nodes are empty structs, so direct conversion
	for i := range nodes {
		result[i] = k_shortest.Node{}
	}
	return result
}
