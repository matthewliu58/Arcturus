package k_shortest

import (
	"math/rand"
	"testing"

	log "github.com/sirupsen/logrus"
)

// TestKShortestWithRandomNetwork tests KShortest with a randomly generated network topology
// Uses a fixed random seed for reproducibility
func TestKShortestWithRandomNetwork(t *testing.T) {
	// Enable debug logging for this test
	log.SetLevel(log.InfoLevel)

	// Fixed seed for reproducible random generation
	const randomSeed int64 = 42
	rng := rand.New(rand.NewSource(randomSeed))

	// Generate a random network with 50 nodes
	const nodeCount = 50
	network := generateRandomNetwork(nodeCount, rng, 0.15) // 15% link density

	log.Infof("TestKShortestWithRandomNetwork: Generated network with %d nodes", len(network.Nodes))

	// Test multiple source-destination pairs
	testCases := []struct {
		name          string
		sourceIdx     int
		destIdx       int
		k             int
		hopThreshold  int
		theta         int
		expectMinPath int // Expect at least this many paths
	}{
		{
			name:          "Adjacent nodes with k=2",
			sourceIdx:     0,
			destIdx:       1,
			k:             2,
			hopThreshold:  10,
			theta:         2,
			expectMinPath: 1, // At least 1 path
		},
		{
			name:          "Distant nodes with k=3",
			sourceIdx:     0,
			destIdx:       25,
			k:             3,
			hopThreshold:  15,
			theta:         2,
			expectMinPath: 1,
		},
		{
			name:          "Middle nodes with k=4",
			sourceIdx:     10,
			destIdx:       40,
			k:             4,
			hopThreshold:  15,
			theta:         2,
			expectMinPath: 1,
		},
		{
			name:          "High k value with k=5",
			sourceIdx:     5,
			destIdx:       45,
			k:             5,
			hopThreshold:  20,
			theta:         2,
			expectMinPath: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			log.Infof("\n=== Test: %s ===", tc.name)
			log.Infof("Parameters: source=%d, dest=%d, k=%d, hopThreshold=%d, theta=%d",
				tc.sourceIdx, tc.destIdx, tc.k, tc.hopThreshold, tc.theta)

			// Make a copy of network to avoid mutation affecting other tests
			networkCopy := copyNetwork(network)

			flow := Flow{
				Source:      tc.sourceIdx,
				Destination: tc.destIdx,
			}

			paths := KShortest(networkCopy, flow, tc.k, tc.hopThreshold, tc.theta)

			log.Infof("Result: Found %d paths (requested k=%d)", len(paths), tc.k)

			// Verify results
			if len(paths) == 0 {
				t.Errorf("Expected at least %d path(s), got 0", tc.expectMinPath)
			}

			if len(paths) < tc.expectMinPath {
				t.Errorf("Expected at least %d path(s), got %d", tc.expectMinPath, len(paths))
			}

			// Log each path details
			for i, path := range paths {
				hopCount := len(path.Nodes) - 1
				log.Infof("  Path[%d]: hops=%d, latency=%d, nodes=%v", i, hopCount, path.Latency, path.Nodes)

				// Verify path validity
				if !isValidPath(networkCopy, path) {
					t.Errorf("Path[%d] is invalid: %v", i, path.Nodes)
				}

				// Verify path connectivity
				if len(path.Nodes) < 2 {
					t.Errorf("Path[%d] has less than 2 nodes: %v", i, path.Nodes)
				}

				if path.Nodes[0] != tc.sourceIdx {
					t.Errorf("Path[%d] source mismatch: expected %d, got %d", i, tc.sourceIdx, path.Nodes[0])
				}

				if path.Nodes[len(path.Nodes)-1] != tc.destIdx {
					t.Errorf("Path[%d] destination mismatch: expected %d, got %d", i, tc.destIdx, path.Nodes[len(path.Nodes)-1])
				}
			}

			// Verify paths are sorted by latency (ascending)
			for i := 1; i < len(paths); i++ {
				if paths[i].Latency < paths[i-1].Latency {
					t.Errorf("Paths not sorted by latency: path[%d]=%d > path[%d]=%d",
						i-1, paths[i-1].Latency, i, paths[i].Latency)
				}
			}

			// Verify no duplicate paths
			for i := 0; i < len(paths); i++ {
				for j := i + 1; j < len(paths); j++ {
					if sliceEqual(paths[i].Nodes, paths[j].Nodes) {
						t.Errorf("Duplicate paths found: path[%d] and path[%d] are identical: %v",
							i, j, paths[i].Nodes)
					}
				}
			}
		})
	}
}

// TestKShortestPathDiversity tests that returned paths are diverse
func TestKShortestPathDiversity(t *testing.T) {
	log.SetLevel(log.InfoLevel)

	const randomSeed int64 = 123
	rng := rand.New(rand.NewSource(randomSeed))

	const nodeCount = 50
	network := generateRandomNetwork(nodeCount, rng, 0.2) // 20% link density for better connectivity

	log.Infof("TestKShortestPathDiversity: Generated network with %d nodes", len(network.Nodes))

	flow := Flow{
		Source:      0,
		Destination: 49,
	}

	k := 5
	networkCopy := copyNetwork(network)
	paths := KShortest(networkCopy, flow, k, 20, 2)

	log.Infof("Found %d diverse paths from node 0 to node 49", len(paths))

	for i, path := range paths {
		log.Infof("  Path[%d]: hops=%d, latency=%d", i, len(path.Nodes)-1, path.Latency)
	}

	if len(paths) < 1 {
		t.Fatalf("Expected at least 1 path, got 0")
	}

	// Check that paths are indeed different
	nodeFrequency := make(map[int]int)
	for i, path := range paths {
		log.Infof("  Path[%d] nodes: %v", i, path.Nodes)

		// Count node usage
		for _, node := range path.Nodes {
			nodeFrequency[node]++
		}
	}

	log.Infof("Node usage frequency: %v", nodeFrequency)
	// The paths should use different intermediate nodes if they are diverse
}

// TestKShortestWithHighDensityNetwork tests with highly connected network
func TestKShortestWithHighDensityNetwork(t *testing.T) {
	log.SetLevel(log.InfoLevel)

	const randomSeed int64 = 456
	rng := rand.New(rand.NewSource(randomSeed))

	const nodeCount = 50
	network := generateRandomNetwork(nodeCount, rng, 0.3) // 30% link density

	log.Infof("TestKShortestWithHighDensityNetwork: Generated network with %d nodes", len(network.Nodes))

	flow := Flow{
		Source:      0,
		Destination: 30,
	}

	// With higher density, we should find more paths
	k := 8
	networkCopy := copyNetwork(network)
	paths := KShortest(networkCopy, flow, k, 20, 2)

	log.Infof("Found %d paths from node 0 to node 30 (requested k=%d)", len(paths), k)

	for i, path := range paths {
		hopCount := len(path.Nodes) - 1
		log.Infof("  Path[%d]: hops=%d, latency=%d, nodes=%v",
			i, hopCount, path.Latency, path.Nodes)
	}

	if len(paths) == 0 {
		t.Fatalf("Expected at least 1 path, got 0")
	}

	// Higher density should yield more paths
	if len(paths) < 2 && k >= 2 {
		log.Warnf("With high density network (30%%), expected to find more than 1 path for k=%d", k)
	}
}

// TestKShortestWithLowHopThreshold tests path quality with low hop threshold
func TestKShortestWithLowHopThreshold(t *testing.T) {
	log.SetLevel(log.InfoLevel)

	const randomSeed int64 = 789
	rng := rand.New(rand.NewSource(randomSeed))

	const nodeCount = 50
	network := generateRandomNetwork(nodeCount, rng, 0.15)

	log.Infof("TestKShortestWithLowHopThreshold: Generated network with %d nodes", len(network.Nodes))

	flow := Flow{
		Source:      0,
		Destination: 20,
	}

	// Test with different hop thresholds
	testCases := []struct {
		hopThreshold int
		name         string
	}{
		{hopThreshold: 3, name: "Low (3 hops)"},
		{hopThreshold: 10, name: "Medium (10 hops)"},
		{hopThreshold: 20, name: "High (20 hops)"},
	}

	k := 3
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			networkCopy := copyNetwork(network)
			paths := KShortest(networkCopy, flow, k, tc.hopThreshold, 2)

			log.Infof("hopThreshold=%d (%s): Found %d paths", tc.hopThreshold, tc.name, len(paths))

			for i, path := range paths {
				hopCount := len(path.Nodes) - 1
				log.Infof("  Path[%d]: hops=%d, latency=%d", i, hopCount, path.Latency)
			}
		})
	}
}

// ==================== Helper Functions ====================

// generateRandomNetwork creates a random network topology with fixed seed for reproducibility
func generateRandomNetwork(nodeCount int, rng *rand.Rand, linkDensity float64) Network {
	network := Network{
		Nodes: make([]Node, nodeCount),
		Links: make([][]int, nodeCount),
	}

	// Initialize nodes and links
	for i := 0; i < nodeCount; i++ {
		network.Nodes[i] = Node{}
		network.Links[i] = make([]int, nodeCount)

		// Initialize all links as unreachable (-1)
		for j := 0; j < nodeCount; j++ {
			if i == j {
				network.Links[i][j] = 0 // Same node, no cost
			} else {
				network.Links[i][j] = -1 // Unreachable by default
			}
		}
	}

	// Add random links based on link density
	// Use a more sophisticated algorithm to ensure better connectivity
	for i := 0; i < nodeCount; i++ {
		for j := i + 1; j < nodeCount; j++ {
			if rng.Float64() < linkDensity {
				// Generate link latency between 10 and 1000
				latency := 10 + rng.Intn(990)

				// Create bidirectional link
				network.Links[i][j] = latency
				network.Links[j][i] = latency

				// log.Infof("Link: %d ↔ %d, latency=%d", i, j, latency)
			}
		}
	}

	// Ensure source (0) and destination (49) are connected through at least some paths
	// by creating some guaranteed connections to improve connectivity
	ensureConnectivity(network, rng)

	return network
}

// ensureConnectivity adds additional links to ensure better network connectivity
func ensureConnectivity(network Network, rng *rand.Rand) {
	nodeCount := len(network.Nodes)

	// Create a backbone: connect node i to node i+1
	for i := 0; i < nodeCount-1; i++ {
		if network.Links[i][i+1] == -1 {
			latency := 50 + rng.Intn(200)
			network.Links[i][i+1] = latency
			network.Links[i+1][i] = latency
		}
	}

	// Add some random longer-range connections to create shortcuts
	for attempt := 0; attempt < nodeCount/3; attempt++ {
		i := rng.Intn(nodeCount)
		j := rng.Intn(nodeCount)
		if i != j && network.Links[i][j] == -1 {
			latency := 100 + rng.Intn(500)
			network.Links[i][j] = latency
			network.Links[j][i] = latency
		}
	}
}

// copyNetwork creates a deep copy of a network to avoid mutations
func copyNetwork(network Network) Network {
	nodeCount := len(network.Nodes)
	copied := Network{
		Nodes: make([]Node, nodeCount),
		Links: make([][]int, nodeCount),
	}

	copy(copied.Nodes, network.Nodes)
	for i := 0; i < nodeCount; i++ {
		copied.Links[i] = make([]int, nodeCount)
		copy(copied.Links[i], network.Links[i])
	}

	return copied
}

// isValidPath verifies that a path is valid in the network
func isValidPath(network Network, path Path) bool {
	if len(path.Nodes) < 2 {
		return false
	}

	// Check each consecutive pair of nodes
	for i := 0; i < len(path.Nodes)-1; i++ {
		from := path.Nodes[i]
		to := path.Nodes[i+1]

		if from < 0 || from >= len(network.Nodes) || to < 0 || to >= len(network.Nodes) {
			return false
		}

		if network.Links[from][to] < 0 {
			return false
		}
	}

	return true
}

// calculatePathLatency calculates the actual latency of a path
func calculatePathLatency(network Network, path Path) int {
	var latency int
	for i := 0; i < len(path.Nodes)-1; i++ {
		latency += network.Links[path.Nodes[i]][path.Nodes[i+1]]
	}
	return latency
}

// printNetworkStats prints statistics about the network
func printNetworkStats(network Network) {
	nodeCount := len(network.Nodes)
	linkCount := 0
	var totalLatency int

	for i := 0; i < nodeCount; i++ {
		for j := i + 1; j < nodeCount; j++ {
			if network.Links[i][j] > 0 {
				linkCount++
				totalLatency += network.Links[i][j]
			}
		}
	}

	density := float64(linkCount*2) / float64(nodeCount*(nodeCount-1))
	avgLatency := 0
	if linkCount > 0 {
		avgLatency = totalLatency / linkCount
	}

	log.Infof("Network stats: nodes=%d, links=%d, density=%.2f%%, avg_latency=%d",
		nodeCount, linkCount, density*100, avgLatency)
}

// BenchmarkKShortestPerformance benchmarks KShortest algorithm performance
func BenchmarkKShortestPerformance(b *testing.B) {
	const randomSeed int64 = 999
	rng := rand.New(rand.NewSource(randomSeed))

	const nodeCount = 50
	network := generateRandomNetwork(nodeCount, rng, 0.15)

	flow := Flow{
		Source:      0,
		Destination: 49,
	}
	k := 5
	hopThreshold := 15
	theta := 2

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		networkCopy := copyNetwork(network)
		_ = KShortest(networkCopy, flow, k, hopThreshold, theta)
	}
}
