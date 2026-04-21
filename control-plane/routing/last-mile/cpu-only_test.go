package last_mile

import (
	agg "control-plane/aggregator"
	rece "control-plane/receive-info"
	"control-plane/routing/routing"
	"log/slog"
	"os"
	"sort"
	"testing"
)

func TestCPUOnlyRouter_Computing(t *testing.T) {
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
			Cpu: rece.CPUInfo{
				Usage: 30.0, // Lowest CPU
			},
		},
		"192.168.1.2": {
			PublicIP:   "192.168.1.2",
			Continent:  "Asia",
			City:       "Beijing",
			Country:    "China",
			CpuPressure: 70.0,
			Cpu: rece.CPUInfo{
				Usage: 70.0, // Highest CPU
			},
		},
		"192.168.1.3": {
			PublicIP:   "192.168.1.3",
			Continent:  "Asia",
			City:       "Guangzhou",
			Country:    "China",
			CpuPressure: 50.0,
			Cpu: rece.CPUInfo{
				Usage: 50.0, // Medium CPU
			},
		},
	}

	router := NewCPUOnlyRouter(nodeTel)
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

	// Verify paths are sorted by CPU score (ascending)
	for i := 0; i < len(paths)-1; i++ {
		if paths[i].Rtt > paths[i+1].Rtt {
			t.Errorf("Paths not sorted by CPU score: path[%d].Rtt=%f > path[%d].Rtt=%f",
				i, paths[i].Rtt, i+1, paths[i+1].Rtt)
		}
	}

	// Best node should have lowest CPU (192.168.1.1 with 30%)
	if paths[0].Hops[0] != "192.168.1.1" {
		t.Errorf("Expected best node 192.168.1.1 (CPU=30%%), got %s", paths[0].Hops[0])
	}
}

func TestCPUOnlyRouter_NoMatchingContinent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:   "192.168.1.1",
			Continent:  "Asia",
			CpuPressure: 30.0,
			Cpu: rece.CPUInfo{
				Usage: 30.0,
			},
		},
	}

	router := NewCPUOnlyRouter(nodeTel)
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

func TestCPUOnlyRouter_NegativeCPU(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{
		"192.168.1.1": {
			PublicIP:   "192.168.1.1",
			Continent:  "Asia",
			CpuPressure: 50.0,
			Cpu: rece.CPUInfo{
				Usage: -1.0, // Invalid negative value
			},
		},
	}

	router := NewCPUOnlyRouter(nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia"},
	}

	paths, err := router.Computing(endPoints, "test", logger)
	if err != nil {
		t.Errorf("Computing failed: %v", err)
	}

	// Negative CPU should be treated as 100
	if paths[0].Rtt != 100.0 {
		t.Errorf("Expected negative CPU to be treated as 100, got %f", paths[0].Rtt)
	}
}

func TestCPUOnlyRouter_MissingTelemetry(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Empty telemetry map
	router := NewCPUOnlyRouter(map[string]*agg.NodeTelemetry{})
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Asia"},
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

func TestCPUOnlyRouter_SortOrder(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	nodeTel := map[string]*agg.NodeTelemetry{}
	for i := 1; i <= 5; i++ {
		ip := "192.168.1." + string(rune('0'+i))
		nodeTel[ip] = &agg.NodeTelemetry{
			PublicIP:   ip,
			Continent:  "Europe",
			CpuPressure: float64(100 - i*10),
			Cpu: rece.CPUInfo{
				Usage: float64(100 - i*10), // 90, 80, 70, 60, 50
			},
		}
	}

	router := NewCPUOnlyRouter(nodeTel)
	endPoints := routing.EndPoints{
		Source: routing.EndPoint{Continent: "Europe"},
	}

	paths, _ := router.Computing(endPoints, "test", logger)

	// Verify sorted by CPU ascending (lowest first)
	expected := []float64{50, 60, 70, 80, 90}
	for i, p := range paths {
		if p.Rtt != expected[i] {
			t.Errorf("Position %d: expected Rtt %f, got %f", i, expected[i], p.Rtt)
		}
	}

	// Verify stable: same CPU should maintain order
	if !sort.SliceIsSorted(paths, func(i, j int) bool {
		return paths[i].Rtt < paths[j].Rtt
	}) {
		t.Error("Paths not correctly sorted")
	}
}
