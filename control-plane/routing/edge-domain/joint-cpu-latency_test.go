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
	util.Config_ = &util.Config{
		Node: util.NodeConfig{
			IP: util.NodeIP{
				Public: "192.168.1.1",
			},
		},
	}
}

func TestJointRouter_Computing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Mock node telemetry data
	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			City:        "Shanghai",
			Country:     "China",
			CpuPressure: 30.0,
			Cpu:         rece.CPUInfo{Usage: 30.0}, // Low CPU
		},
		"192.168.1.2": {
			PublicIP:    "192.168.1.2",
			Continent:   "Asia",
			City:        "Beijing",
			Country:     "China",
			CpuPressure: 70.0,
			Cpu:         rece.CPUInfo{Usage: 70.0}, // High CPU
		},
		"192.168.1.3": {
			PublicIP:    "192.168.1.3",
			Continent:   "Asia",
			City:        "Guangzhou",
			Country:     "China",
			CpuPressure: 50.0,
			Cpu:         rece.CPUInfo{Usage: 50.0}, // Medium CPU
		},
	}

	// Mock edge congestion data
	edgeAgg := map[string]*rece.LastCongestion{
		"Shanghai-192.168.1.1": {AvgRT: 80.0, Count: 10}, // High latency, low CPU
		"Shanghai-192.168.1.2": {AvgRT: 40.0, Count: 10}, // Low latency, high CPU
		"Shanghai-192.168.1.3": {AvgRT: 60.0, Count: 10}, // Medium both
	}

	router := NewJointRouter(edgeAgg, nodeTel)
	router.SetWeights(0.5, 0.5) // Equal weights

	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	if len(paths) != 3 {
		t.Errorf("Expected 3 paths, got %d", len(paths))
	}

	// Verify paths are sorted by combined score
	for i := 0; i < len(paths)-1; i++ {
		if paths[i].Rtt > paths[i+1].Rtt {
			t.Errorf("Paths not sorted by score: path[%d].Rtt=%f > path[%d].Rtt=%f",
				i, paths[i].Rtt, i+1, paths[i+1].Rtt)
		}
	}

	// With 50/50 weights:
	// node1: 0.5*30 + 0.5*80 = 55
	// node2: 0.5*70 + 0.5*40 = 55
	// node3: 0.5*50 + 0.5*60 = 55
	// All equal, first in list wins
	t.Logf("Best node: %s with score %f", paths[0].Hops[0], paths[0].Rtt)
}

func TestJointRouter_CPUHeavy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			CpuPressure: 30.0,
			Cpu:         rece.CPUInfo{Usage: 30.0}, // Best CPU
		},
		"192.168.1.2": {
			PublicIP:    "192.168.1.2",
			Continent:   "Asia",
			CpuPressure: 90.0,
			Cpu:         rece.CPUInfo{Usage: 90.0}, // Worst CPU
		},
	}

	edgeAgg := map[string]*rece.LastCongestion{
		"Asia-192.168.1.1": {AvgRT: 100.0, Count: 10}, // High latency
		"Asia-192.168.1.2": {AvgRT: 40.0, Count: 10},  // Low latency
	}

	router := NewJointRouter(edgeAgg, nodeTel)
	router.SetWeights(1.0, 0.0) // CPU only

	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia"},
	}

	paths, _ := router.Computing(endPoints, "test", logger)

	// Should pick low CPU node regardless of latency
	if paths[0].Hops[0] != "192.168.1.1" {
		t.Errorf("Expected 192.168.1.1 (low CPU), got %s", paths[0].Hops[0])
	}
}

func TestJointRouter_LatencyHeavy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			CpuPressure: 90.0,
			Cpu:         rece.CPUInfo{Usage: 90.0}, // High CPU
		},
		"192.168.1.2": {
			PublicIP:    "192.168.1.2",
			Continent:   "Asia",
			CpuPressure: 30.0,
			Cpu:         rece.CPUInfo{Usage: 30.0}, // Low CPU
		},
	}

	// Key format: "Continent-NodeIP"
	edgeAgg := map[string]*rece.LastCongestion{
		"Asia-192.168.1.1": {AvgRT: 40.0, Count: 10},  // Low latency
		"Asia-192.168.1.2": {AvgRT: 100.0, Count: 10}, // High latency
	}

	router := NewJointRouter(edgeAgg, nodeTel)
	router.SetWeights(0.0, 1.0) // Latency only

	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia"},
	}

	paths, _ := router.Computing(endPoints, "test", logger)

	// Should pick low latency node regardless of CPU
	if paths[0].Hops[0] != "192.168.1.1" {
		t.Errorf("Expected 192.168.1.1 (low latency), got %s", paths[0].Hops[0])
	}
}

func TestJointRouter_SetWeights(t *testing.T) {
	tests := []struct {
		cpuW     float64
		latencyW float64
		wantCpu  float64
		wantLat  float64
	}{
		{0.5, 0.5, 0.5, 0.5},
		{1.0, 1.0, 0.5, 0.5}, // Normalized
		{0.0, 1.0, 0.0, 1.0},
		{1.0, 0.0, 1.0, 0.0},
		{0.0, 0.0, 0.5, 0.5},  // Invalid, should keep defaults (0.5, 0.5)
		{-1.0, 1.0, 0.5, 0.5}, // Invalid, should keep defaults (0.5, 0.5)
		{0.3, 0.7, 0.3, 0.7},  // Custom ratio
	}

	for _, tt := range tests {
		// Create new router each time to avoid state pollution
		router := NewJointRouter(
			map[string]*rece.LastCongestion{},
			map[string]*agg.NodeTelemetry{},
		)
		router.SetWeights(tt.cpuW, tt.latencyW)
		if router.cpuWeight != tt.wantCpu || router.latencyWeight != tt.wantLat {
			t.Errorf("SetWeights(%f, %f) = (%f, %f), want (%f, %f)",
				tt.cpuW, tt.latencyW, router.cpuWeight, router.latencyWeight, tt.wantCpu, tt.wantLat)
		}
	}
}

func TestJointRouter_NegativeValues(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			CpuPressure: 50.0,
			Cpu:         rece.CPUInfo{Usage: -1.0}, // Invalid negative CPU -> treated as 100
		},
	}

	// Empty edgeAgg: GetNodeRT returns &LastCongestion{} (AvgRT=0)
	// delay <= 0 -> treated as 100
	edgeAgg := map[string]*rece.LastCongestion{}

	router := NewJointRouter(edgeAgg, nodeTel)
	router.SetWeights(0.5, 0.5)

	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	// Negative CPU (-1) -> treated as 100
	// Missing latency stats (Count=0) -> delay=500 (code default)
	// score = 0.5*100 + 0.5*500 = 300
	if paths[0].Rtt != 300.0 {
		t.Errorf("Expected score 300, got %f", paths[0].Rtt)
	}
}

func TestJointRouter_NoMatchingContinent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			CpuPressure: 30.0,
			Cpu:         rece.CPUInfo{Usage: 30.0},
		},
	}

	router := NewJointRouter(map[string]*rece.LastCongestion{}, nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "NorthAmerica", City: "New York"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Should not return error: %v", err)
	}

	// Should fallback to default node
	if len(paths) != 1 {
		t.Errorf("Expected 1 fallback path, got %d", len(paths))
	}
}

func TestJointRouter_GetNodeRT(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:    "192.168.1.1",
			Continent:   "Asia",
			Country:     "China",
			City:        "Shanghai",
			CpuPressure: 30.0,
			Cpu:         rece.CPUInfo{Usage: 30.0},
		},
	}

	edgeAgg := map[string]*rece.LastCongestion{
		"Shanghai-192.168.1.1": {AvgRT: 40.0, Count: 10},
		"Shanghai-Shanghai":    {AvgRT: 50.0, Count: 10},
		"China-China":          {AvgRT: 60.0, Count: 10},
		"Asia-Asia":            {AvgRT: 70.0, Count: 10},
		"Asia-general":         {AvgRT: 80.0, Count: 10},
	}

	router := NewJointRouter(edgeAgg, nodeTel)
	source := routing.EndPoint{Continent: "Asia", Country: "China", City: "Shanghai"}

	stats := router.GetNodeRT(source, "192.168.1.1", "test", logger)
	if stats == nil {
		t.Fatal("GetNodeRT returned nil")
	}

	// Should match exact city-IP first
	if stats.AvgRT != 40.0 {
		t.Errorf("Expected AvgRT 40.0 (city-IP match), got %f", stats.AvgRT)
	}
}
