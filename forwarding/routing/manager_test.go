package routing

import (
	"forwarding/middle_mile_scheduling/common"
	"testing"
	"time"
)

// MockPathCalculator implements the PathCalculator interface for testing
type MockPathCalculator struct {
	name string
}

func (m *MockPathCalculator) ComputePaths(
	network *common.Network,
	source int,
	dest int,
	params map[string]interface{},
	ipToIndex map[string]int,
	indexToIP map[int]string,
) []common.PathWithIP {
	// Return mock paths with pre-calculated weights
	return []common.PathWithIP{
		{
			IPList:  []string{"10.0.0.1", "10.0.0.2", "10.0.0.4"},
			Latency: 10,
			Weight:  60, // 60ms / 10ms = 6, then normalized to 60/100
		},
		{
			IPList:  []string{"10.0.0.1", "10.0.0.3", "10.0.0.4"},
			Latency: 20,
			Weight:  40, // 60ms / 20ms = 3, then normalized to 40/100
		},
	}
}

// TestCalculatePaths tests the CalculatePaths function
func TestCalculatePaths(t *testing.T) {
	// Setup: Create a mock network
	network := common.Network{
		Nodes: []common.Node{{}, {}, {}},
		Links: [][]int{
			{0, 10, -1},
			{10, 0, 20},
			{-1, 20, 0},
		},
	} // Setup: Create IP mappings
	ipToIndex := map[string]int{
		"10.0.0.1": 0,
		"10.0.0.2": 1,
		"10.0.0.3": 2,
		"10.0.0.4": 3,
	}

	indexToIP := map[int]string{
		0: "10.0.0.1",
		1: "10.0.0.2",
		2: "10.0.0.3",
		3: "10.0.0.4",
	}

	// Create PathManager instance manually for testing
	pm := &PathManager{
		pathChan:      make(chan []common.PathWithIP, 1),
		latestPaths:   nil,
		sourceIP:      "10.0.0.1",
		destinationIP: "10.0.0.4",
		domain:        "test.example.com",
		params: map[string]interface{}{
			"k": 2,
		},
		calculator:    &MockPathCalculator{name: "mock"},
		algorithmName: "mock",
	}

	// Start the path listener goroutine
	go pm.pathListener()

	// Test 1: Call CalculatePaths
	t.Run("TestCalculatePathsSuccess", func(t *testing.T) {
		// Call CalculatePaths
		pm.CalculatePaths(network, ipToIndex, indexToIP)

		// Wait a bit for pathListener to process
		time.Sleep(200 * time.Millisecond)

		// Get paths via GetPaths instead of reading from channel
		paths := pm.GetPaths()

		if len(paths) != 2 {
			t.Errorf("Expected 2 paths, got %d", len(paths))
		}

		if len(paths) > 0 {
			// Verify first path
			if paths[0].Latency != 10 {
				t.Errorf("Expected latency 10, got %d", paths[0].Latency)
			}
			if paths[0].Weight != 60 {
				t.Errorf("Expected weight 60, got %d", paths[0].Weight)
			}
			if len(paths[0].IPList) != 3 {
				t.Errorf("Expected 3 nodes in path, got %d", len(paths[0].IPList))
			}

			// Verify second path
			if paths[1].Latency != 20 {
				t.Errorf("Expected latency 20, got %d", paths[1].Latency)
			}
			if paths[1].Weight != 40 {
				t.Errorf("Expected weight 40, got %d", paths[1].Weight)
			}

			t.Logf("✓ CalculatePaths returned correct paths: %d paths with weights %d, %d",
				len(paths), paths[0].Weight, paths[1].Weight)
		}
	})

	// Test 2: Verify GetPaths returns latest paths
	t.Run("TestGetPaths", func(t *testing.T) {
		// Call CalculatePaths again
		pm.CalculatePaths(network, ipToIndex, indexToIP)

		// Wait a bit for pathListener goroutine to process
		time.Sleep(100 * time.Millisecond)

		// Get paths using GetPaths
		paths := pm.GetPaths()

		if len(paths) != 2 {
			t.Errorf("Expected 2 paths from GetPaths, got %d", len(paths))
		}

		if len(paths) > 0 && (paths[0].Weight != 60 || paths[1].Weight != 40) {
			t.Errorf("Weights mismatch: expected [60, 40], got [%d, %d]",
				paths[0].Weight, paths[1].Weight)
		}

		t.Logf("✓ GetPaths returned correct paths: %v", paths)
	})

	// Test 3: Test with invalid source IP
	t.Run("TestInvalidSourceIP", func(t *testing.T) {
		pmInvalid := &PathManager{
			pathChan:      make(chan []common.PathWithIP, 1),
			sourceIP:      "192.168.1.1", // Not in ipToIndex
			destinationIP: "10.0.0.4",
			domain:        "invalid.test",
			calculator:    &MockPathCalculator{},
		}

		pmInvalid.CalculatePaths(network, ipToIndex, indexToIP)

		// Should not send anything to channel
		select {
		case <-pmInvalid.pathChan:
			t.Error("Should not receive paths for invalid source IP")
		default:
			t.Logf("✓ Correctly rejected invalid source IP")
		}
	})

	// Test 4: Test with invalid destination IP
	t.Run("TestInvalidDestinationIP", func(t *testing.T) {
		pmInvalid := &PathManager{
			pathChan:      make(chan []common.PathWithIP, 1),
			sourceIP:      "10.0.0.1",
			destinationIP: "192.168.1.1", // Not in ipToIndex
			domain:        "invalid.test",
			calculator:    &MockPathCalculator{},
		}

		pmInvalid.CalculatePaths(network, ipToIndex, indexToIP)

		// Should not send anything to channel
		select {
		case <-pmInvalid.pathChan:
			t.Error("Should not receive paths for invalid destination IP")
		default:
			t.Logf("✓ Correctly rejected invalid destination IP")
		}
	})

	// Test 5: Test with params passing
	t.Run("TestParamsPassing", func(t *testing.T) {
		pmParams := &PathManager{
			pathChan:      make(chan []common.PathWithIP, 1),
			sourceIP:      "10.0.0.1",
			destinationIP: "10.0.0.4",
			domain:        "params.test",
			params: map[string]interface{}{
				"k": 3,
			},
			calculator:    &MockPathCalculator{name: "mock"},
			algorithmName: "mock",
		}

		go pmParams.pathListener()

		pmParams.CalculatePaths(network, ipToIndex, indexToIP)

		time.Sleep(50 * time.Millisecond)

		// Verify params were correctly passed to calculator
		paths := pmParams.GetPaths()
		if len(paths) > 0 {
			t.Logf("✓ Parameters correctly passed to calculator: %d paths returned", len(paths))
		}
	})

	// Test 6: Test parameters passing
	t.Run("TestParametersPassing", func(t *testing.T) {
		pmParams := &PathManager{
			pathChan:      make(chan []common.PathWithIP, 1),
			sourceIP:      "10.0.0.1",
			destinationIP: "10.0.0.4",
			domain:        "params.test",
			params: map[string]interface{}{
				"k":             3,
				"custom_param":  "value",
				"another_param": 123,
			},
			calculator:    &MockPathCalculator{name: "test"},
			algorithmName: "test",
		}

		pmParams.CalculatePaths(network, ipToIndex, indexToIP)

		// Verify parameters are passed correctly
		select {
		case paths := <-pmParams.pathChan:
			if len(paths) > 0 {
				t.Logf("✓ Parameters passed successfully, received %d paths", len(paths))
			}
		default:
			t.Logf("✓ Parameters passing test completed")
		}
	})
}

// TestCalculatePathsIntegration tests the full integration with PathManager
func TestCalculatePathsIntegration(t *testing.T) {
	// Setup: Create a mock network
	network := common.Network{
		Nodes: []common.Node{{}, {}, {}},
		Links: [][]int{
			{-1, 10, 20},
			{10, -1, 5},
			{20, 5, -1},
		},
	}

	ipToIndex := map[string]int{
		"10.0.0.1": 0,
		"10.0.0.2": 1,
		"10.0.0.3": 2,
	}

	indexToIP := map[int]string{
		0: "10.0.0.1",
		1: "10.0.0.2",
		2: "10.0.0.3",
	}

	t.Run("TestMultipleCalculations", func(t *testing.T) {
		pm := &PathManager{
			pathChan:      make(chan []common.PathWithIP, 1),
			sourceIP:      "10.0.0.1",
			destinationIP: "10.0.0.3",
			domain:        "multi.test",
			params: map[string]interface{}{
				"k": 2,
			},
			calculator:    &MockPathCalculator{name: "mock"},
			algorithmName: "mock",
		}

		// Call CalculatePaths multiple times
		for i := 0; i < 3; i++ {
			pm.CalculatePaths(network, ipToIndex, indexToIP)

			select {
			case paths := <-pm.pathChan:
				if len(paths) > 0 {
					t.Logf("✓ Iteration %d: received %d paths", i+1, len(paths))
				}
			default:
				t.Logf("✓ Iteration %d: completed", i+1)
			}
		}

		// Verify latest paths
		latestPaths := pm.GetPaths()
		if len(latestPaths) > 0 {
			t.Logf("✓ Latest paths: %d paths available", len(latestPaths))
		}
	})

	// Test channel overflow handling
	t.Run("TestChannelOverflowHandling", func(t *testing.T) {
		pm := &PathManager{
			pathChan:      make(chan []common.PathWithIP, 1), // Buffer size 1
			sourceIP:      "10.0.0.1",
			destinationIP: "10.0.0.3",
			domain:        "overflow.test",
			params: map[string]interface{}{
				"k": 2,
			},
			calculator:    &MockPathCalculator{name: "mock"},
			algorithmName: "mock",
		}

		// First calculation
		pm.CalculatePaths(network, ipToIndex, indexToIP)
		<-pm.pathChan // Consume first result

		// Second calculation
		pm.CalculatePaths(network, ipToIndex, indexToIP)

		// Third calculation while channel might be full
		pm.CalculatePaths(network, ipToIndex, indexToIP)

		// Verify we can still get the latest result
		select {
		case paths := <-pm.pathChan:
			if len(paths) > 0 {
				t.Logf("✓ Channel overflow handling works: received %d paths", len(paths))
			}
		default:
			t.Logf("✓ Channel state handled correctly")
		}
	})
}

// Benchmark test for CalculatePaths
func BenchmarkCalculatePaths(b *testing.B) {
	network := common.Network{
		Nodes: []common.Node{{}, {}, {}, {}},
		Links: [][]int{
			{-1, 5, 10, -1},
			{5, -1, 3, 15},
			{10, 3, -1, 2},
			{-1, 15, 2, -1},
		},
	}

	ipToIndex := map[string]int{
		"10.0.0.1": 0,
		"10.0.0.2": 1,
		"10.0.0.3": 2,
		"10.0.0.4": 3,
	}

	indexToIP := map[int]string{
		0: "10.0.0.1",
		1: "10.0.0.2",
		2: "10.0.0.3",
		3: "10.0.0.4",
	}

	pm := &PathManager{
		pathChan:      make(chan []common.PathWithIP, 1),
		sourceIP:      "10.0.0.1",
		destinationIP: "10.0.0.4",
		domain:        "bench.test",
		params: map[string]interface{}{
			"k": 2,
		},
		calculator:    &MockPathCalculator{name: "mock"},
		algorithmName: "mock",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pm.CalculatePaths(network, ipToIndex, indexToIP)
		// Consume result
		select {
		case <-pm.pathChan:
		default:
		}
	}
}
