package edge_domain

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

	router := NewP2CRouter(nodeTel, nil)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Fatalf("Computing failed: %v", err)
	}

	if len(paths) != 3 {
		t.Fatalf("Expected 3 paths (proportional distribution), got %d", len(paths))
	}

	// Verify probabilities sum to 1.0
	sumProb := 0.0
	probMap := make(map[string]float64)
	for _, p := range paths {
		sumProb += p.Rtt
		probMap[p.Hops[0]] = p.Rtt
	}
	if sumProb < 0.99 || sumProb > 1.01 {
		t.Errorf("Probabilities sum = %f, expected ~1.0", sumProb)
	}

	// CPU 30% should get highest prob, CPU 70% should get lowest
	if probMap["192.168.1.1"] <= probMap["192.168.1.3"] {
		t.Errorf("Node with CPU=30%% should have higher prob than CPU=50%%: %v", probMap)
	}
	if probMap["192.168.1.3"] <= probMap["192.168.1.2"] {
		t.Errorf("Node with CPU=50%% should have higher prob than CPU=70%%: %v", probMap)
	}

	t.Logf("P2C probs: %v", probMap)
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

	router := NewP2CRouter(nodeTel, nil)
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

	router := NewP2CRouter(nodeTel, nil)
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

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			City:        "Shanghai",
			Country:     "China",
			CpuPressure: -1.0,
			Cpu: rece.CPUInfo{
				Usage: -1.0, // Invalid → treated as 100
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

	router := NewP2CRouter(nodeTel, nil)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Fatalf("Computing failed: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("Expected 2 paths, got %d", len(paths))
	}

	// CPU=50% should get higher prob than negative CPU (treated as 100%)
	probMap := make(map[string]float64)
	for _, p := range paths {
		probMap[p.Hops[0]] = p.Rtt
	}

	if probMap["192.168.1.2"] <= probMap["192.168.1.1"] {
		t.Errorf("Node with CPU=50%% should have higher prob than negative CPU (100%%): %v", probMap)
	}

	t.Logf("Negative CPU probs: %v", probMap)
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

	router := NewP2CRouter(nodeTel, nil)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Fatalf("Computing failed: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("Expected 2 paths, got %d", len(paths))
	}

	// P2C returns all nodes with proportional weights (no sorting)
	// Just verify both nodes present and probs sum to 1
	sumProb := paths[0].Rtt + paths[1].Rtt
	if sumProb < 0.99 || sumProb > 1.01 {
		t.Errorf("Probabilities sum = %f, expected ~1.0", sumProb)
	}

	// Each prob should be in (0, 1)
	for _, p := range paths {
		if p.Rtt <= 0 || p.Rtt >= 1 {
			t.Errorf("Probability %f for node %s out of (0,1) range", p.Rtt, p.Hops[0])
		}
	}

	t.Logf("P2C path sorting test: %s=%.4f, %s=%.4f",
		paths[0].Hops[0], paths[0].Rtt, paths[1].Hops[0], paths[1].Rtt)
}

func TestP2CRouter_ProportionalWeights(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// 3 nodes: weight = 1/CPU, probs should match
	// CPU 30 → 1/30 = 0.0333
	// CPU 50 → 1/50 = 0.02
	// CPU 70 → 1/70 = 0.0143
	// total = 0.0676
	// prob: 30→49.3%, 50→29.6%, 70→21.1%
	nodeTel := map[string]*agg.NodeTelemetry{
		"10.0.0.1": {PublicIP: "10.0.0.1", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 30.0}},
		"10.0.0.2": {PublicIP: "10.0.0.2", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 50.0}},
		"10.0.0.3": {PublicIP: "10.0.0.3", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 70.0}},
	}

	router := NewP2CRouter(nodeTel, nil)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Tokyo"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Fatalf("Computing failed: %v", err)
	}

	if len(paths) != 3 {
		t.Fatalf("Expected 3 paths, got %d", len(paths))
	}

	// Verify probabilities sum to 1
	sumProb := 0.0
	probMap := make(map[string]float64)
	for _, p := range paths {
		sumProb += p.Rtt
		probMap[p.Hops[0]] = p.Rtt
	}
	if sumProb < 0.99 || sumProb > 1.01 {
		t.Errorf("Probabilities sum = %f, expected ~1.0", sumProb)
	}

	// Exact verification
	expected := map[string]float64{
		"10.0.0.1": 0.493, // 1/30 / (1/30+1/50+1/70)
		"10.0.0.2": 0.296, // 1/50 / ...
		"10.0.0.3": 0.211, // 1/70 / ...
	}
	for ip, exp := range expected {
		got := probMap[ip]
		if got < exp-0.02 || got > exp+0.02 {
			t.Errorf("Node %s: expected prob ~%.4f, got %.4f", ip, exp, got)
		}
	}

	t.Logf("Proportional weights: %v", probMap)
}

func TestP2CRouter_WithEdgeAgg(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"10.0.0.1": {PublicIP: "10.0.0.1", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 30.0}},
		"10.0.0.2": {PublicIP: "10.0.0.2", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 70.0}},
	}

	edgeAgg := map[string]*rece.LastCongestion{
		"Tokyo-10.0.0.1": {AvgRT: 15.0, Count: 100},
		"Tokyo-10.0.0.2": {AvgRT: 80.0, Count: 100},
	}

	router := NewP2CRouter(nodeTel, edgeAgg)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Tokyo"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Fatalf("Computing failed: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("Expected 2 paths, got %d", len(paths))
	}

	// Verify probabilities sum to 1
	sumProb := paths[0].Rtt + paths[1].Rtt
	if sumProb < 0.99 || sumProb > 1.01 {
		t.Errorf("Probabilities sum = %f, expected ~1.0", sumProb)
	}

	// P2C only uses CPU for weight, latency is just for logging
	// So probs should match 1/CPU ratio: 30% vs 70%
	probMap := make(map[string]float64)
	for _, p := range paths {
		probMap[p.Hops[0]] = p.Rtt
	}
	if probMap["10.0.0.1"] <= probMap["10.0.0.2"] {
		t.Errorf("Lower CPU node should get higher prob: %v", probMap)
	}

	t.Logf("P2C with edgeAgg: %v", probMap)
}

func TestP2CRouter_ZeroCPUHandling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"10.0.0.1": {PublicIP: "10.0.0.1", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 0.0}}, // 0 → treated as 100
		"10.0.0.2": {PublicIP: "10.0.0.2", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 50.0}},
	}

	router := NewP2CRouter(nodeTel, nil)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Tokyo"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Fatalf("Computing failed: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("Expected 2 paths, got %d", len(paths))
	}

	// CPU=0 → load=100, so CPU=50 should get higher prob
	probMap := make(map[string]float64)
	for _, p := range paths {
		probMap[p.Hops[0]] = p.Rtt
	}

	if probMap["10.0.0.2"] <= probMap["10.0.0.1"] {
		t.Errorf("CPU=50%% should have higher prob than CPU=0 (treated as 100%%): %v", probMap)
	}

	t.Logf("Zero CPU probs: %v", probMap)
}
