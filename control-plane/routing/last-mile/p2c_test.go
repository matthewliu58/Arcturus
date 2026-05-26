package last_mile

import (
	agg "control-plane/aggregator"
	rece "control-plane/receive-info"
	"control-plane/routing/routing"
	"control-plane/util"
	"log/slog"
	"os"
	"testing"
)

func init() {
	// Initialize util.Config_ for tests
	util.Config_ = &util.Config{
		Node: util.NodeConfig{
			IP: util.NodeIP{
				Public: "192.168.1.1",
			},
		},
	}
}

func TestP2CRouter_BasicFunctionality(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Mock node telemetry data with different CPU loads
	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			City:        "Shanghai",
			Country:     "China",
			CpuPressure: 30.0,
			Cpu: rece.CPUInfo{
				Usage: 30.0, // Lowest CPU
			},
		},
		"192.168.1.2": {
			PublicIP:    "192.168.1.2",
			Continent:   "Asia",
			City:        "Beijing",
			Country:     "China",
			CpuPressure: 70.0,
			Cpu: rece.CPUInfo{
				Usage: 70.0, // Highest CPU
			},
		},
		"192.168.1.3": {
			PublicIP:    "192.168.1.3",
			Continent:   "Asia",
			City:        "Guangzhou",
			Country:     "China",
			CpuPressure: 50.0,
			Cpu: rece.CPUInfo{
				Usage: 50.0, // Medium CPU
			},
		},
	}

	router := NewP2CRouter(nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	// Run multiple times to test randomness
	selectedCounts := make(map[string]int)
	numRuns := 100

	for i := 0; i < numRuns; i++ {
		paths, err := router.Computing(endPoints, "test", logger)
		if err != nil {
			t.Errorf("Computing failed: %v", err)
		}

		// Should return at least 1 path
		if len(paths) < 1 {
			t.Errorf("Expected at least 1 path, got %d", len(paths))
		}

		// Best path should always be the lowest CPU node
		if paths[0].Hops[0] != "192.168.1.1" {
			t.Errorf("Run %d: Expected best node 192.168.1.1 (CPU=30%%), got %s", i, paths[0].Hops[0])
		}

		// Count selections
		selectedCounts[paths[0].Hops[0]]++
	}

	// Best node should be selected most often (but not always due to randomness)
	t.Logf("Selection counts over %d runs: %v", numRuns, selectedCounts)

	// With P2C, the best node should be selected >50% of the time
	// (it's not guaranteed 100% due to random sampling)
}

func TestP2CRouter_SingleNode(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			City:        "Shanghai",
			Country:     "China",
			CpuPressure: 40.0,
			Cpu: rece.CPUInfo{
				Usage: 40.0,
			},
		},
	}

	router := NewP2CRouter(nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	// With single node, should return exactly 1 path
	if len(paths) != 1 {
		t.Errorf("Expected 1 path for single node, got %d", len(paths))
	}

	if paths[0].Hops[0] != "192.168.1.1" {
		t.Errorf("Expected node 192.168.1.1, got %s", paths[0].Hops[0])
	}
}

func TestP2CRouter_NoMatchingContinent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			City:        "Shanghai",
			Country:     "China",
			CpuPressure: 30.0,
			Cpu: rece.CPUInfo{
				Usage: 30.0,
			},
		},
	}

	router := NewP2CRouter(nodeTel)
	// Request from Europe, but only Asia nodes available
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Europe", City: "London"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	// Should fallback to local node
	if len(paths) != 1 {
		t.Errorf("Expected 1 path with fallback, got %d", len(paths))
	}

	if paths[0].Hops[0] != util.Config_.Node.IP.Public {
		t.Errorf("Expected fallback node %s, got %s", util.Config_.Node.IP.Public, paths[0].Hops[0])
	}
}

func TestP2CRouter_NegativeCPUHandling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Test handling of negative CPU values (should be treated as 100)
	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			City:        "Shanghai",
			Country:     "China",
			CpuPressure: -1.0, // Invalid
			Cpu: rece.CPUInfo{
				Usage: -1.0, // Should be treated as 100
			},
		},
		"192.168.1.2": {
			PublicIP:    "192.168.1.2",
			Continent:   "Asia",
			City:        "Beijing",
			Country:     "China",
			CpuPressure: 50.0,
			Cpu: rece.CPUInfo{
				Usage: 50.0,
			},
		},
	}

	router := NewP2CRouter(nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	// Should skip or handle negative CPU correctly
	// Best node should be 192.168.1.2 with CPU=50%
	if len(paths) > 0 && paths[0].Hops[0] != "192.168.1.2" {
		t.Errorf("Expected best node 192.168.1.2 (CPU=50%%), got %s", paths[0].Hops[0])
	}
}

func TestP2CRouter_PathSorting(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			City:        "Shanghai",
			Country:     "China",
			CpuPressure: 30.0,
			Cpu: rece.CPUInfo{
				Usage: 30.0,
			},
		},
		"192.168.1.2": {
			PublicIP:    "192.168.1.2",
			Continent:   "Asia",
			City:        "Beijing",
			Country:     "China",
			CpuPressure: 70.0,
			Cpu: rece.CPUInfo{
				Usage: 70.0,
			},
		},
	}

	router := NewP2CRouter(nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	// Verify paths are sorted by load (ascending)
	for i := 0; i < len(paths)-1; i++ {
		if paths[i].Rtt > paths[i+1].Rtt {
			t.Errorf("Paths not sorted by load: path[%d].Rtt=%f > path[%d].Rtt=%f",
				i, paths[i].Rtt, i+1, paths[i+1].Rtt)
		}
	}
}
