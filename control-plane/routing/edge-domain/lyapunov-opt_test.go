package edge_domain

import (
	agg "control-plane/aggregator"
	"control-plane/routing/routing"
	"control-plane/util"
	"fmt"
	"log/slog"
	"math"
	"os"
	"testing"

	rece "control-plane/receive-info"
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

	// Verify paths are sorted by score (lower score = better, so RawRTT ascending)
	for i := 0; i < len(paths)-1; i++ {
		if paths[i].RawRTT > paths[i+1].RawRTT {
			t.Errorf("Paths not sorted by score: path[%d].RawRTT=%f > path[%d].RawRTT=%f", i, paths[i].RawRTT, i+1, paths[i+1].RawRTT)
		}
	}

	// The first path should be the one with best balance of CPU and latency
	// In this case, 192.168.1.1 has low CPU load and low latency
	expectedBestNode := "192.168.1.1"
	if len(paths) > 0 && paths[0].Hops[0] != expectedBestNode {
		t.Errorf("Expected best node %s, got %s", expectedBestNode, paths[0].Hops[0])
	}
}

const epsilon = 1e-4

func TestCPUPenalty(t *testing.T) {
	// Test 2-core thresholds: Mid=60
	// cpuPenalty = exp(2.0 * (Qk/60 - 1))
	testCases2Core := []struct {
		Qk           float64
		logicalCores int
		expected     float64
	}{
		{30.0, 2, 0.367879},  // exp(2.0*(30/60-1)) = exp(-1.0)
		{50.0, 2, 0.716531},  // exp(2.0*(50/60-1)) = exp(-0.333)
		{70.0, 2, 1.395612},  // exp(2.0*(70/60-1)) = exp(0.333)
		{90.0, 2, 2.718282},  // exp(2.0*(90/60-1)) = exp(1.0)
	}

	// Test 4-core thresholds: Mid=80, α=3.0
	// cpuPenalty = exp(3.0 * (Qk/80 - 1))
	testCases4Core := []struct {
		Qk           float64
		logicalCores int
		expected     float64
	}{
		{50.0, 4, 0.324652},  // exp(3.0*(50/80-1)) = exp(-1.125)
		{70.0, 4, 0.687289},  // exp(3.0*(70/80-1)) = exp(-0.375)
		{90.0, 4, 1.454991},  // exp(3.0*(90/80-1)) = exp(0.375)
		{100.0, 4, 2.117000}, // exp(3.0*(100/80-1)) = exp(0.75)
	}

	for _, tc := range testCases2Core {
		result := computeCPUPenalty(tc.Qk, tc.logicalCores)
		if math.Abs(result-tc.expected) > epsilon {
			t.Errorf("computeCPUPenalty(%f, %d) = %f, expected %f", tc.Qk, tc.logicalCores, result, tc.expected)
		}
	}

	for _, tc := range testCases4Core {
		result := computeCPUPenalty(tc.Qk, tc.logicalCores)
		if math.Abs(result-tc.expected) > epsilon {
			t.Errorf("computeCPUPenalty(%f, %d) = %f, expected %f", tc.Qk, tc.logicalCores, result, tc.expected)
		}
	}
}

func TestDelayPenalty(t *testing.T) {
	// delayPenalty = exp(0.20 * (rt/good - 1))
	// Using good=20 (intra-continental default)
	testCases := []struct {
		rt       float64
		good     float64
		expected float64
	}{
		{10.0, 20, 0.904837},  // exp(0.20*(10/20-1)) = exp(-0.1)
		{20.0, 20, 1.0},        // exp(0.20*(20/20-1)) = exp(0) = 1
		{40.0, 20, 1.221403},  // exp(0.20*(40/20-1)) = exp(0.2)
		{60.0, 20, 1.491825},  // exp(0.20*(60/20-1)) = exp(0.4)

		// Inter-continental: good=60
		{60.0, 60, 1.0},        // exp(0.20*(60/60-1)) = exp(0) = 1
		{100.0, 60, 1.142617},  // exp(0.20*(100/60-1)) = exp(0.1333)
		{150.0, 60, 1.349859},  // exp(0.20*(150/60-1)) = exp(0.3)
	}

	for _, tc := range testCases {
		config := latencyConfig{good: tc.good}
		result := computeDelayPenalty(tc.rt, config)
		if math.Abs(result-tc.expected) > epsilon {
			t.Errorf("computeDelayPenalty(%f, good=%f) = %f, expected %f", tc.rt, tc.good, result, tc.expected)
		}
	}
}

func TestLyapunovSolver_ThreeNodes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Configuration for 3 nodes:
	// Node1: 2 cores, CPU 40%, deltaK=10, latency=10ms
	// Node2: 2 cores, CPU 60%, deltaK=10, latency=15ms
	// Node3: 4 cores, CPU 80%, deltaK=20, latency=5ms
	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:  "192.168.1.1",
			Continent: "Asia",
			City:      "Shanghai",
			Cpu: rece.CPUInfo{
				Usage:       40.0, // 40%
				LoadDelta:   10.0,
				LogicalCore: 2,
			},
		},
		"192.168.1.2": {
			PublicIP:  "192.168.1.2",
			Continent: "Asia",
			City:      "Beijing",
			Cpu: rece.CPUInfo{
				Usage:       60.0, // 60%
				LoadDelta:   10.0,
				LogicalCore: 2,
			},
		},
		"192.168.1.3": {
			PublicIP:  "192.168.1.3",
			Continent: "Asia",
			City:      "Guangzhou",
			Cpu: rece.CPUInfo{
				Usage:       80.0, // 80%
				LoadDelta:   20.0,
				LogicalCore: 4,
			},
		},
	}

	// Latency data
	edgeAgg := map[string]*rece.LastCongestion{
		"Shanghai-192.168.1.1": {AvgRT: 10.0, Count: 10}, // 10ms
		"Shanghai-192.168.1.2": {AvgRT: 15.0, Count: 10}, // 15ms
		"Shanghai-192.168.1.3": {AvgRT: 5.0, Count: 10},  // 5ms
	}

	solver := NewLyapunovSolver(edgeAgg, nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := solver.Computing(endPoints, "test-3nodes", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	if len(paths) != 3 {
		t.Fatalf("Expected 3 paths, got %d", len(paths))
	}

	// Print raw scores and softmax probabilities
	fmt.Println("\n=== Three Nodes Test Results ===")
	fmt.Println("Node Configuration:")
	fmt.Println("  Node1 (192.168.1.1): 2 cores, CPU=40%, deltaK=10, latency=10ms")
	fmt.Println("  Node2 (192.168.1.2): 2 cores, CPU=60%, deltaK=10, latency=15ms")
	fmt.Println("  Node3 (192.168.1.3): 4 cores, CPU=80%, deltaK=20, latency=5ms")
	fmt.Println("\nRaw Scores (lower is better):")

	for _, path := range paths {
		fmt.Printf("  %s: RawScore=%f, SoftmaxProbability=%f\n", path.Hops[0], path.Rtt, path.RawRTT)
	}

	// Verify softmax probabilities sum to 1
	sumWeight := 0.0
	for _, path := range paths {
		sumWeight += path.Rtt
	}
	if sumWeight < 0.999 || sumWeight > 1.001 {
		t.Errorf("Softmax probabilities sum = %.4f, expected ~1.0", sumWeight)
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
