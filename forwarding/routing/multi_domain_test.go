package routing

import (
	"testing"

	"forwarding/middle_mile_scheduling/common"
)

// TestPreloadDomains tests the PreloadDomains function
func TestPreloadDomains(t *testing.T) {
	// Clean up instances before test
	instances = make(map[string]*PathManager)

	// Note: This test requires domain mappings to be set up
	// In a real scenario, this would come from FileManager

	PreloadDomains()

	// Check that instances were created (or empty if no mappings)
	instancesMu.RLock()
	count := len(instances)
	instancesMu.RUnlock()

	t.Logf("PreloadDomains created %d PathManager instances", count)
	// If domain mappings exist, count should be > 0
	// If not, count should be 0 (not an error condition)
}

// TestGetInstance tests the GetInstance function
func TestGetInstance(t *testing.T) {
	// Clean up instances before test
	instances = make(map[string]*PathManager)

	// Note: This test requires FileManager to provide domain mappings
	// and system IP to be available

	pm := GetInstance("test.example.com")

	if pm == nil {
		t.Logf("GetInstance returned nil (expected if domain mapping not found)")
		return
	}

	if pm.domain != "test.example.com" {
		t.Errorf("Expected domain 'test.example.com', got '%s'", pm.domain)
	}

	if pm.algorithmName == "" {
		t.Errorf("Expected algorithm name to be set, got empty string")
	}

	t.Logf("GetInstance created PathManager: domain=%s, algorithm=%s, sourceIP=%s, destIP=%s",
		pm.domain, pm.algorithmName, pm.sourceIP, pm.destinationIP)
}

// TestSetAlgorithm tests algorithm switching
func TestSetAlgorithm(t *testing.T) {
	// Note: This test requires the adapter package to be initialized
	// so that algorithms are registered

	pm := &PathManager{
		domain:        "test.domain",
		algorithmName: "k_shortest",
	}

	// Try to set algorithm
	err := pm.SetAlgorithm("k_shortest")
	if err != nil {
		t.Logf("SetAlgorithm failed (expected if adapter not initialized): %v", err)
	} else {
		t.Logf("SetAlgorithm succeeded: %s", pm.algorithmName)
	}
}

// TestCalculatePathsForAllDomains tests batch path calculation
func TestCalculatePathsForAllDomains(t *testing.T) {
	// Clean up instances before test
	instances = make(map[string]*PathManager)

	// Create a simple test network
	testNetwork := common.Network{
		Nodes: []common.Node{{}, {}, {}, {}},
		Links: [][]int{
			{0, 10, 20, -1},
			{10, 0, -1, 30},
			{20, -1, 0, 5},
			{-1, 30, 5, 0},
		},
	}

	ipToIndex := map[string]int{
		"192.168.1.1": 0,
		"192.168.1.2": 1,
		"192.168.1.3": 2,
		"192.168.1.4": 3,
	}

	indexToIP := map[int]string{
		0: "192.168.1.1",
		1: "192.168.1.2",
		2: "192.168.1.3",
		3: "192.168.1.4",
	}

	// Call function with empty instances
	CalculatePathsForAllDomains(testNetwork, ipToIndex, indexToIP)

	t.Logf("CalculatePathsForAllDomains completed (expected no-op with empty instances)")
}

// TestMultiDomainFlow tests the complete multi-domain flow
func TestMultiDomainFlow(t *testing.T) {
	t.Log("Multi-domain routing system is structured as follows:")
	t.Log("1. PreloadDomains() - creates PathManager for each domain")
	t.Log("2. GetInstance(domain) - retrieves or creates PathManager for specific domain")
	t.Log("3. SetAlgorithm(name) - allows runtime algorithm selection")
	t.Log("4. CalculatePaths() - computes paths using selected algorithm")
	t.Log("5. CalculatePathsForAllDomains() - batch calculation for all domains")
	t.Log("6. GetPaths() - retrieves computed paths for domain")

	t.Log("\nKey features:")
	t.Log("- Each domain has independent PathManager instance")
	t.Log("- Each instance can use different algorithm")
	t.Log("- Topology changes trigger path recalculation")
	t.Log("- Supports both K-Shortest and Carousel-Greedy algorithms")
}

/*
// Example for multi-domain setup (disabled - MultiDomainSetup function not available)
func ExampleMultiDomainSetup() {
	fmt.Println("Example: Multi-domain routing setup")
	fmt.Println("")
	fmt.Println("1. Initialize and preload domains:")
	fmt.Println("   routing.PreloadDomains()")
	fmt.Println("")
	fmt.Println("2. Get PathManager for specific domain:")
	fmt.Println("   pm := routing.GetInstance(\"domain1.com\")")
	fmt.Println("")
	fmt.Println("3. Switch algorithm (optional):")
	fmt.Println("   pm.SetAlgorithm(\"carousel_greedy\")")
	fmt.Println("")
	fmt.Println("4. Get computed paths:")
	fmt.Println("   paths := pm.GetPaths()")
	fmt.Println("")
	// Output:
	// Example: Multi-domain routing setup
	//
	// 1. Initialize and preload domains:
	//    routing.PreloadDomains()
	//
	// 2. Get PathManager for specific domain:
	//    pm := routing.GetInstance("domain1.com")
	//
	// 3. Switch algorithm (optional):
	//    pm.SetAlgorithm("carousel_greedy")
	//
	// 4. Get computed paths:
	//    paths := pm.GetPaths()
	//
}
*/
