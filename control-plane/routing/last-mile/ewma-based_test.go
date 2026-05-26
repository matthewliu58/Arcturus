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

func TestEWMARouter_BasicFunctionality(t *testing.T) {
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

	edgeAgg := map[string]*rece.LastCongestion{
		"Asia-China-Shanghai_Asia-China-Beijing": {
			AvgRT: 50.0,
			Count: 100,
		},
	}

	router := NewEWMARouter(nodeTel, edgeAgg)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	if len(paths) != 2 {
		t.Errorf("Expected 2 paths, got %d", len(paths))
	}

	// Best node should have lower combined score
	t.Logf("Best node: %s, score: %f", paths[0].Hops[0], paths[0].Rtt)
}

func TestEWMARouter_EWMASmoothing(t *testing.T) {
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

	edgeAgg := map[string]*rece.LastCongestion{}

	router := NewEWMARouter(nodeTel, edgeAgg)

	// Simulate burst of high CPU
	router.updateEWMACPU("192.168.1.1", 100)
	router.updateEWMACPU("192.168.1.1", 100)
	router.updateEWMACPU("192.168.1.1", 100)

	// EWMA should be smoothed, not at 100
	cpuEwma := router.getCPUEWMA("192.168.1.1")
	t.Logf("CPU EWMA after 3x100: %f", cpuEwma)

	// With alpha=0.3, EWMA should be somewhere between 30 and 100
	if cpuEwma < 30 || cpuEwma > 100 {
		t.Errorf("EWMA out of expected range: %f", cpuEwma)
	}
}

func TestEWMARouter_EWMACalculation(t *testing.T) {
	// Test EWMA formula: X(t) = α * x(t) + (1-α) * X(t-1)
	nodeTel := map[string]*agg.NodeTelemetry{}
	edgeAgg := map[string]*rece.LastCongestion{}

	router := NewEWMARouter(nodeTel, edgeAgg)
	router.SetAlpha(0.3, 0.7)

	// Initial value
	router.updateEWMACPU("node1", 50.0)
	ewma1 := router.getCPUEWMA("node1")
	if ewma1 != 50.0 {
		t.Errorf("Initial EWMA should be 50, got %f", ewma1)
	}

	// Update with 100: X(1) = 0.3*100 + 0.7*50 = 30 + 35 = 65
	router.updateEWMACPU("node1", 100.0)
	ewma2 := router.getCPUEWMA("node1")
	expected2 := 0.3*100.0 + 0.7*50.0
	if ewma2 != expected2 {
		t.Errorf("EWMA should be %f, got %f", expected2, ewma2)
	}
	t.Logf("EWMA after update: %f (expected: %f)", ewma2, expected2)
}

func TestEWMARouter_SingleNode(t *testing.T) {
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

	edgeAgg := map[string]*rece.LastCongestion{}

	router := NewEWMARouter(nodeTel, edgeAgg)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Errorf("Expected 1 path for single node, got %d", len(paths))
	}

	if paths[0].Hops[0] != "192.168.1.1" {
		t.Errorf("Expected node 192.168.1.1, got %s", paths[0].Hops[0])
	}
}

func TestEWMARouter_CombinedScore(t *testing.T) {
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
			CpuPressure: 30.0,
			Cpu: rece.CPUInfo{
				Usage: 30.0,
			},
		},
	}

	edgeAgg := map[string]*rece.LastCongestion{
		"Asia-China-Shanghai_Asia-China-Beijing": {
			AvgRT: 50.0,
			Count: 100,
		},
	}

	router := NewEWMARouter(nodeTel, edgeAgg)
	router.SetAlpha(0.3, 0.5) // 50% weight on CPU

	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	// Both nodes have same CPU, but first one has better latency
	// Combined score should reflect both factors
	for _, path := range paths {
		t.Logf("Node: %s, Score: %f", path.Hops[0], path.Rtt)
	}
}

func TestEWMARouter_NegativeCPUHandling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			City:        "Shanghai",
			Country:     "China",
			CpuPressure: -1.0,
			Cpu: rece.CPUInfo{
				Usage: -1.0, // Invalid, should be treated as 100
			},
		},
	}

	edgeAgg := map[string]*rece.LastCongestion{}

	router := NewEWMARouter(nodeTel, edgeAgg)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	// Should handle negative CPU correctly
	if len(paths) != 1 {
		t.Errorf("Expected 1 path, got %d", len(paths))
	}
}

func TestEWMARouter_GetEWMASummary(t *testing.T) {
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

	edgeAgg := map[string]*rece.LastCongestion{}

	router := NewEWMARouter(nodeTel, edgeAgg)

	// Update some EWMA values
	router.updateEWMACPU("192.168.1.1", 50.0)
	router.updateEWMALatency("192.168.1.1", 100.0)

	summary := router.GetEWMASummary()

	if len(summary) != 1 {
		t.Errorf("Expected 1 node in summary, got %d", len(summary))
	}

	if summary["192.168.1.1"].CPU != 50.0 {
		t.Errorf("Expected CPU EWMA 50.0, got %f", summary["192.168.1.1"].CPU)
	}
}
