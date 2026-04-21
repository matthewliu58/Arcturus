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

func TestLatencyOnlyRouter_Computing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Mock node telemetry data
	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:   "192.168.1.1",
			Continent:  "Asia",
			City:       "Shanghai",
			Country:    "China",
			CpuPressure: 30.0,
			Cpu:        rece.CPUInfo{Usage: 30.0},
		},
		"192.168.1.2": {
			PublicIP:   "192.168.1.2",
			Continent:  "Asia",
			City:       "Beijing",
			Country:    "China",
			CpuPressure: 30.0,
			Cpu:        rece.CPUInfo{Usage: 30.0},
		},
		"192.168.1.3": {
			PublicIP:   "192.168.1.3",
			Continent:  "Asia",
			City:       "Guangzhou",
			Country:    "China",
			CpuPressure: 30.0,
			Cpu:        rece.CPUInfo{Usage: 30.0},
		},
	}

	// Mock edge congestion data (latency from Shanghai to each node)
	edgeAgg := map[string]*rece.LastCongestion{
		"Shanghai-192.168.1.1": {AvgRT: 80.0, Count: 10}, // Worst latency
		"Shanghai-192.168.1.2": {AvgRT: 40.0, Count: 10}, // Best latency
		"Shanghai-192.168.1.3": {AvgRT: 60.0, Count: 10}, // Medium latency
	}

	router := NewLatencyOnlyRouter(edgeAgg, nodeTel)
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

	// Verify paths are sorted by latency (ascending)
	for i := 0; i < len(paths)-1; i++ {
		if paths[i].Rtt > paths[i+1].Rtt {
			t.Errorf("Paths not sorted by latency: path[%d].Rtt=%f > path[%d].Rtt=%f",
				i, paths[i].Rtt, i+1, paths[i+1].Rtt)
		}
	}

	// Best node should have lowest latency (192.168.1.2 with 40ms)
	if paths[0].Hops[0] != "192.168.1.2" {
		t.Errorf("Expected best node 192.168.1.2 (latency=40ms), got %s", paths[0].Hops[0])
	}
}

func TestLatencyOnlyRouter_NoMatchingContinent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:   "192.168.1.1",
			Continent:  "Asia",
			CpuPressure: 30.0,
			Cpu:       rece.CPUInfo{Usage: 30.0},
		},
	}

	router := NewLatencyOnlyRouter(map[string]*rece.LastCongestion{}, nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "North America", City: "New York"},
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

func TestLatencyOnlyRouter_NoRTStats(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:   "192.168.1.1",
			Continent:  "Asia",
			CpuPressure: 30.0,
			Cpu:        rece.CPUInfo{Usage: 30.0},
		},
	}

	// No latency stats available
	edgeAgg := map[string]*rece.LastCongestion{}

	router := NewLatencyOnlyRouter(edgeAgg, nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	// Should use default latency of 500 when no stats
	if len(paths) > 0 && paths[0].Rtt != 500.0 {
		t.Errorf("Expected default latency 500, got %f", paths[0].Rtt)
	}
}

func TestLatencyOnlyRouter_NegativeLatency(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:   "192.168.1.1",
			Continent:  "Asia",
			CpuPressure: 30.0,
			Cpu:       rece.CPUInfo{Usage: 30.0},
		},
	}

	edgeAgg := map[string]*rece.LastCongestion{
		"Shanghai-192.168.1.1": {AvgRT: -10.0, Count: 10}, // Invalid negative latency
	}

	router := NewLatencyOnlyRouter(edgeAgg, nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Shanghai"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	// Negative latency should be treated as 100
	if paths[0].Rtt != 100.0 {
		t.Errorf("Expected negative latency to be treated as 100, got %f", paths[0].Rtt)
	}
}

func TestLatencyOnlyRouter_GetNodeRT(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:   "192.168.1.1",
			Continent:  "Asia",
			Country:    "China",
			City:       "Shanghai",
			CpuPressure: 30.0,
			Cpu:       rece.CPUInfo{Usage: 30.0},
		},
	}

	edgeAgg := map[string]*rece.LastCongestion{
		"Shanghai-192.168.1.1": {AvgRT: 40.0, Count: 10}, // Exact city-IP match
		"Shanghai-Shanghai":     {AvgRT: 50.0, Count: 10}, // City-city match
		"China-China":           {AvgRT: 60.0, Count: 10}, // Country-country match
		"Asia-Asia":             {AvgRT: 70.0, Count: 10}, // Continent-continent match
		"Asia-general":          {AvgRT: 80.0, Count: 10}, // Continent-general match
	}

	router := NewLatencyOnlyRouter(edgeAgg, nodeTel)
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

func TestLatencyOnlyRouter_FallbackOrder(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:   "192.168.1.1",
			Continent:  "Asia",
			Country:    "Japan",
			City:       "Tokyo",
			CpuPressure: 30.0,
			Cpu:        rece.CPUInfo{Usage: 30.0},
		},
	}

	// Only continent-general fallback available
	edgeAgg := map[string]*rece.LastCongestion{
		"Asia-general": {AvgRT: 100.0, Count: 10},
	}

	router := NewLatencyOnlyRouter(edgeAgg, nodeTel)
	source := routing.EndPoint{Continent: "Asia", Country: "China", City: "Beijing"}

	stats := router.GetNodeRT(source, "192.168.1.1", "test", logger)
	if stats == nil {
		t.Fatal("GetNodeRT returned nil")
	}

	// Should fallback to continent-general
	if stats.AvgRT != 100.0 {
		t.Errorf("Expected AvgRT 100.0 (continent-general fallback), got %f", stats.AvgRT)
	}
}

func TestLatencyOnlyRouter_EmptyEdgeAgg(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:   "192.168.1.1",
			Continent:  "Asia",
			CpuPressure: 30.0,
			Cpu:        rece.CPUInfo{Usage: 30.0},
		},
	}

	router := NewLatencyOnlyRouter(map[string]*rece.LastCongestion{}, nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia", City: "Beijing"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	// Should return default empty congestion
	if paths[0].Rtt != 500.0 {
		t.Errorf("Expected default latency 500, got %f", paths[0].Rtt)
	}
}
