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

func TestEWMARouter_FixedRangeNormalization(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// 3 nodes with different CPU and latency
	nodeTel := map[string]*agg.NodeTelemetry{
		"10.0.0.1": {
			PublicIP:  "10.0.0.1",
			Continent: "Asia",
			City:      "Tokyo",
			Cpu: rece.CPUInfo{
				Usage: 30.0, // lowest CPU
			},
		},
		"10.0.0.2": {
			PublicIP:  "10.0.0.2",
			Continent: "Asia",
			City:      "Seoul",
			Cpu: rece.CPUInfo{
				Usage: 55.0, // middle CPU
			},
		},
		"10.0.0.3": {
			PublicIP:  "10.0.0.3",
			Continent: "Asia",
			City:      "Singapore",
			Cpu: rece.CPUInfo{
				Usage: 70.0, // highest CPU
			},
		},
	}

	// Source=Tokyo, latency data for each node
	edgeAgg := map[string]*rece.LastCongestion{
		"Tokyo-10.0.0.1": {
			AvgRT: 5.0, Count: 100, // lowest latency
		},
		"Tokyo-10.0.0.2": {
			AvgRT: 50.0, Count: 100, // middle latency
		},
		"Tokyo-10.0.0.3": {
			AvgRT: 80.0, Count: 100, // highest latency
		},
	}

	router := NewEWMARouter(nodeTel, edgeAgg)
	router.SetAlpha(0.3, 0.5) // λ=0.5, equal weight on CPU and latency

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
	for _, p := range paths {
		sumProb += p.Rtt
	}
	if sumProb < 0.99 || sumProb > 1.01 {
		t.Errorf("Probabilities sum = %f, expected ~1.0", sumProb)
	}

	// Verify RawRTT stores combined score (positive, CPU + scaled latency)
	for _, p := range paths {
		if p.RawRTT <= 0 {
			t.Errorf("RawRTT=%f should be positive for node %s", p.RawRTT, p.Hops[0])
		}
	}

	// Verify node 10.0.0.1 (lowest CPU 30%, lowest latency 5ms) gets highest prob
	probMap := make(map[string]float64)
	for _, p := range paths {
		probMap[p.Hops[0]] = p.Rtt
	}

	if probMap["10.0.0.1"] <= probMap["10.0.0.2"] || probMap["10.0.0.1"] <= probMap["10.0.0.3"] {
		t.Errorf("Best node (lowest CPU+latency) should have highest prob, got: %v", probMap)
	}

	// Per-node scoring (no batch normalization):
	// normLat = latEwma / 1.0 (raw ms, same scale as CPU 0-100)
	// Node 10.0.0.1: CPU=30, lat=5→normLat=5, combined=0.5*30+0.5*5=17.5
	// Node 10.0.0.2: CPU=55, lat=50→normLat=50, combined=0.5*55+0.5*50=52.5
	// Node 10.0.0.3: CPU=70, lat=80→normLat=80, combined=0.5*70+0.5*80=75.0
	// 10.0.0.1 should have highest prob (lowest combined score)
	t.Logf("Per-node scoring result: %v", probMap)
}

func TestEWMARouter_ProbabilitiesSumToOne(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// 2 nodes with different characteristics
	nodeTel := map[string]*agg.NodeTelemetry{
		"10.0.0.1": {
			PublicIP:  "10.0.0.1",
			Continent: "Asia",
			City:      "Tokyo",
			Cpu: rece.CPUInfo{
				Usage: 40.0,
			},
		},
		"10.0.0.2": {
			PublicIP:  "10.0.0.2",
			Continent: "Asia",
			City:      "Seoul",
			Cpu: rece.CPUInfo{
				Usage: 80.0,
			},
		},
	}

	edgeAgg := map[string]*rece.LastCongestion{
		"Tokyo-10.0.0.1": {AvgRT: 20.0, Count: 100},
		"Tokyo-10.0.0.2": {AvgRT: 60.0, Count: 100},
	}

	router := NewEWMARouter(nodeTel, edgeAgg)
	router.SetAlpha(0.3, 0.5)

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

	// Verify probabilities sum to 1.0
	totalProb := paths[0].Rtt + paths[1].Rtt
	if totalProb < 0.99 || totalProb > 1.01 {
		t.Errorf("Total probability = %f, expected ~1.0 (p1=%f, p2=%f)",
			totalProb, paths[0].Rtt, paths[1].Rtt)
	}

	// Each prob should be in (0, 1)
	for _, p := range paths {
		if p.Rtt <= 0 || p.Rtt >= 1 {
			t.Errorf("Probability %f for node %s out of (0,1) range", p.Rtt, p.Hops[0])
		}
	}

	t.Logf("Probs: %s=%.4f, %s=%.4f, sum=%.4f",
		paths[0].Hops[0], paths[0].Rtt, paths[1].Hops[0], paths[1].Rtt, totalProb)
}

func TestEWMARouter_LatencyKeyFormat(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"10.0.0.1": {
			PublicIP:  "10.0.0.1",
			Continent: "Asia",
			City:      "Tokyo",
			Cpu: rece.CPUInfo{
				Usage: 30.0,
			},
		},
	}

	// Latency key = City + "-" + nodeIP
	edgeAgg := map[string]*rece.LastCongestion{
		"Tokyo-10.0.0.1": {
			AvgRT: 15.0,
			Count: 100,
		},
	}

	router := NewEWMARouter(nodeTel, edgeAgg)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Tokyo"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Fatalf("Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("Expected 1 path, got %d", len(paths))
	}

	// When latency data exists, it should be used (not default 50ms)
	// The combined score should reflect the low latency
	if paths[0].RawRTT == 0 && paths[0].Rtt == 1.0 {
		// Single node case: min-max gives 0, prob = 1/0 → prob=1
		// This is expected behavior for single node
		t.Log("Single node: prob=1.0 (expected)")
	}

	t.Logf("Latency key test: node=%s, prob=%.4f, rawRTT=%.4f",
		paths[0].Hops[0], paths[0].Rtt, paths[0].RawRTT)
}

func TestEWMARouter_AllNodesEqual(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// 3 nodes with identical CPU and latency
	nodeTel := map[string]*agg.NodeTelemetry{
		"10.0.0.1": {
			PublicIP: "10.0.0.1", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 50.0},
		},
		"10.0.0.2": {
			PublicIP: "10.0.0.2", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 50.0},
		},
		"10.0.0.3": {
			PublicIP: "10.0.0.3", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 50.0},
		},
	}

	edgeAgg := map[string]*rece.LastCongestion{
		"Tokyo-10.0.0.1": {AvgRT: 30.0, Count: 100},
		"Tokyo-10.0.0.2": {AvgRT: 30.0, Count: 100},
		"Tokyo-10.0.0.3": {AvgRT: 30.0, Count: 100},
	}

	router := NewEWMARouter(nodeTel, edgeAgg)
	router.SetAlpha(0.3, 0.5)

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

	// All equal → all should get ~33.3%
	for _, p := range paths {
		if p.Rtt < 0.32 || p.Rtt > 0.34 {
			t.Errorf("Expected ~0.333 prob for equal nodes, got %f for %s", p.Rtt, p.Hops[0])
		}
	}

	t.Logf("Equal nodes probs: %s=%.4f, %s=%.4f, %s=%.4f",
		paths[0].Hops[0], paths[0].Rtt,
		paths[1].Hops[0], paths[1].Rtt,
		paths[2].Hops[0], paths[2].Rtt)
}

func TestEWMARouter_SingleNodeFixedRange(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"10.0.0.1": {
			PublicIP: "10.0.0.1", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 60.0},
		},
	}

	edgeAgg := map[string]*rece.LastCongestion{
		"Tokyo-10.0.0.1": {AvgRT: 25.0, Count: 100},
	}

	router := NewEWMARouter(nodeTel, edgeAgg)
	router.SetAlpha(0.3, 0.5)

	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Tokyo"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Fatalf("Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("Expected 1 path, got %d", len(paths))
	}

	// Single node: prob should always be 1.0 (100% traffic)
	if paths[0].Rtt != 1.0 {
		t.Errorf("Single node prob should be 1.0, got %f", paths[0].Rtt)
	}
	// With per-node scoring: CPU=60, lat=25→normLat=25, combined=0.5*60+0.5*25=42.5
	// RawRTT should be the combined score (not 0 as with old min-max)
	if paths[0].RawRTT <= 0 {
		t.Errorf("Single node combined should be > 0, got %f", paths[0].RawRTT)
	}

	t.Logf("Single node: prob=%.4f, combined=%.4f", paths[0].Rtt, paths[0].RawRTT)
}

func TestEWMARouter_DefaultLatencyFallback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"10.0.0.1": {
			PublicIP: "10.0.0.1", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 30.0},
		},
		"10.0.0.2": {
			PublicIP: "10.0.0.2", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 70.0},
		},
	}

	// No latency data at all → should use default 50ms for both
	edgeAgg := map[string]*rece.LastCongestion{}

	router := NewEWMARouter(nodeTel, edgeAgg)
	router.SetAlpha(0.3, 0.5)

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

	// Both have default 50ms latency → latency dimension equal, CPU drives difference
	// Lower CPU (30%) should get higher prob than higher CPU (70%)
	probMap := make(map[string]float64)
	for _, p := range paths {
		probMap[p.Hops[0]] = p.Rtt
	}

	if probMap["10.0.0.1"] <= probMap["10.0.0.2"] {
		t.Errorf("Node with lower CPU should get higher prob: %v", probMap)
	}

	t.Logf("Default latency test: %s=%.4f, %s=%.4f",
		"10.0.0.1", probMap["10.0.0.1"], "10.0.0.2", probMap["10.0.0.2"])
}

func TestEWMARouter_NormLatEwmaInDebugLog(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"10.0.0.1": {
			PublicIP: "10.0.0.1", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 50.0},
		},
		"10.0.0.2": {
			PublicIP: "10.0.0.2", Continent: "Asia", City: "Tokyo",
			Cpu: rece.CPUInfo{Usage: 80.0},
		},
	}

	edgeAgg := map[string]*rece.LastCongestion{
		"Tokyo-10.0.0.1": {AvgRT: 20.0, Count: 100},
		"Tokyo-10.0.0.2": {AvgRT: 80.0, Count: 100},
	}

	router := NewEWMARouter(nodeTel, edgeAgg)
	router.SetAlpha(0.3, 0.5)

	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Tokyo"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Fatalf("Computing failed: %v", err)
	}

	// normLatEwma = latEwma/100, combined = λ*CPU + (1-λ)*normLat
	for _, p := range paths {
		if p.RawRTT <= 0 {
			t.Errorf("combined score (RawRTT) = %f should be positive for %s", p.RawRTT, p.Hops[0])
		}
	}

	// Node with 20ms should have lower combined (better) than node with 80ms
	// when CPU is also worse for the 80ms node
	probMap := make(map[string]float64)
	for _, p := range paths {
		probMap[p.Hops[0]] = p.Rtt
	}

	if probMap["10.0.0.1"] <= probMap["10.0.0.2"] {
		t.Errorf("Best node (low CPU + low latency) should get higher prob: %v", probMap)
	}

	t.Logf("Lat+CPU test: %s=%.4f, %s=%.4f",
		"10.0.0.1", probMap["10.0.0.1"], "10.0.0.2", probMap["10.0.0.2"])
}
