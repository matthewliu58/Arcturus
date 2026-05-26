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
	util.Config_ = &util.Config{
		Node: util.NodeConfig{
			IP: util.NodeIP{
				Public: "192.168.1.1",
			},
		},
	}
}

func TestLyapunovSolver_Computing(t *testing.T) {
	// Setup test logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Mock node telemetry data
	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:  "192.168.1.1",
			Continent: "Asia",
			City:      "Shanghai",
			Cpu: rece.CPUInfo{
				Usage:       30.0, // Low load
				LoadDelta:   5.0,
				LogicalCore: 4,
			},
		},
		"192.168.1.2": {
			PublicIP:  "192.168.1.2",
			Continent: "Asia",
			City:      "Beijing",
			Cpu: rece.CPUInfo{
				Usage:       70.0, // High load
				LoadDelta:   10.0,
				LogicalCore: 4,
			},
		},
		"192.168.1.3": {
			PublicIP:  "192.168.1.3",
			Continent: "Asia",
			City:      "Guangzhou",
			Cpu: rece.CPUInfo{
				Usage:       50.0, // Medium load
				LoadDelta:   5.0,
				LogicalCore: 4,
			},
		},
	}

	// Mock edge congestion data
	edgeAgg := map[string]*rece.LastCongestion{
		"Shanghai-192.168.1.1": {AvgRT: 40.0, Count: 10}, // Low latency
		"Shanghai-192.168.1.2": {AvgRT: 60.0, Count: 10}, // Medium latency
		"Shanghai-192.168.1.3": {AvgRT: 80.0, Count: 10}, // High latency
	}

	solver := NewLyapunovSolver(edgeAgg, nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := solver.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	if len(paths) == 0 {
		t.Error("No paths returned")
	}

	// Verify paths are sorted by score (lower is better)
	for i := 0; i < len(paths)-1; i++ {
		if paths[i].Rtt > paths[i+1].Rtt {
			t.Errorf("Paths not sorted by score: path[%d].Rtt=%f > path[%d].Rtt=%f", i, paths[i].Rtt, i+1, paths[i+1].Rtt)
		}
	}

	// The first path should be the one with best balance of CPU and latency
	// In this case, 192.168.1.1 has low CPU load and low latency
	expectedBestNode := "192.168.1.1"
	if len(paths) > 0 && paths[0].Hops[0] != expectedBestNode {
		t.Errorf("Expected best node %s, got %s", expectedBestNode, paths[0].Hops[0])
	}
}

func TestCPUPenalty(t *testing.T) {
	// Test 2-core thresholds: CPULow=40, CPUMid=60, CPUHigh=80
	testCases2Core := []struct {
		nodeIP       string
		Qk           float64
		logicalCores int
		expected     float64
	}{
		{"test-node-1", 30.0, 2, 0.5}, // Low CPU (<40)
		{"test-node-2", 50.0, 2, 1.0}, // Medium CPU (40-60)
		{"test-node-3", 70.0, 2, 2.0}, // High CPU (60-80)
		{"test-node-4", 90.0, 2, 5.0}, // Very high CPU (>80)
	}

	// Test 4-core thresholds: CPULow=60, CPUMid=80, CPUHigh=100
	testCases4Core := []struct {
		nodeIP       string
		Qk           float64
		logicalCores int
		expected     float64
	}{
		{"test-node-5", 50.0, 4, 0.5},  // Low CPU (<60)
		{"test-node-6", 70.0, 4, 1.0},  // Medium CPU (60-80)
		{"test-node-7", 90.0, 4, 2.0},  // High CPU (80-100)
		{"test-node-8", 100.0, 4, 4.0}, // Max CPU (=100)
	}

	for _, tc := range testCases2Core {
		result := computeCPUPenalty(tc.nodeIP, tc.Qk, tc.logicalCores)
		if result != tc.expected {
			t.Errorf("computeCPUPenalty(%s, %f, %d) = %f, expected %f", tc.nodeIP, tc.Qk, tc.logicalCores, result, tc.expected)
		}
	}

	for _, tc := range testCases4Core {
		result := computeCPUPenalty(tc.nodeIP, tc.Qk, tc.logicalCores)
		if result != tc.expected {
			t.Errorf("computeCPUPenalty(%s, %f, %d) = %f, expected %f", tc.nodeIP, tc.Qk, tc.logicalCores, result, tc.expected)
		}
	}

	// Test penalty state persistence
	nodeIP := "test-node-penalty"
	// First call with high CPU should set penalty flag
	computeCPUPenalty(nodeIP, 70.0, 2)
	// Second call with medium CPU should still return high penalty
	result := computeCPUPenalty(nodeIP, 50.0, 2)
	t.Logf("computeCPUPenalty with penalty state = %f", result)

	// Call with low CPU should clear penalty flag
	computeCPUPenalty(nodeIP, 40.0, 2)
	// Next call with medium CPU should return medium penalty
	result = computeCPUPenalty(nodeIP, 50.0, 2)
	expected := 1.0
	if result != expected {
		t.Errorf("computeCPUPenalty after clearing penalty = %f, expected %f", result, expected)
	}
}

func TestDelayPenalty(t *testing.T) {
	testCases := []struct {
		rt       float64
		expected float64
	}{
		{40.0, 0.5},  // Good latency
		{80.0, 1.0},  // Normal latency
		{140.0, 1.5}, // Warning latency
		{160.0, 2.2}, // Bad latency
		{200.0, 3.0}, // Very bad latency
	}

	config := latencyConfig{
		good:    DefaultLatencyGood,
		normal:  DefaultLatencyNormal,
		warning: DefaultLatencyWarning,
	}

	for _, tc := range testCases {
		result := computeDelayPenalty(tc.rt, config)
		if result != tc.expected {
			t.Errorf("computeDelayPenalty(%f) = %f, expected %f", tc.rt, result, tc.expected)
		}
	}
}

func TestLyapunovSolver_GetNodeRT(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Mock node telemetry data
	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:  "192.168.1.1",
			Continent: "Asia",
			Country:   "China",
			City:      "Shanghai",
		},
	}

	// Mock edge congestion data
	edgeAgg := map[string]*rece.LastCongestion{
		"Shanghai-192.168.1.1": {AvgRT: 40.0, Count: 10}, // Exact city-node IP
		"Shanghai-Shanghai":    {AvgRT: 50.0, Count: 10}, // City-city
		"China-China":          {AvgRT: 60.0, Count: 10}, // Country-country
		"Asia-Asia":            {AvgRT: 70.0, Count: 10}, // Continent-continent
		"Asia-general":         {AvgRT: 80.0, Count: 10}, // Continent-general
	}

	solver := NewLyapunovSolver(edgeAgg, nodeTel)

	// Test exact city-node IP match
	source := routing.EndPoint{Continent: "Asia", Country: "China", City: "Shanghai"}
	stats := solver.GetNodeRT(source, "192.168.1.1", "test", logger)
	if stats == nil || stats.AvgRT != 40.0 {
		t.Errorf("Expected AvgRT 40.0, got %v", stats)
	}

	// Test fallback to continent-continent when no specific match
	source2 := routing.EndPoint{Continent: "Asia", Country: "Japan", City: "Tokyo"}
	stats2 := solver.GetNodeRT(source2, "192.168.1.1", "test", logger)
	// The fallback should match "Asia-Asia" first, not "Asia-general"
	if stats2 == nil || stats2.AvgRT != 70.0 {
		t.Errorf("Expected AvgRT 70.0 for fallback, got %v", stats2)
	}
}
