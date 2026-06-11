package core_domain

import (
	"control-plane/routing/graph"
	"log/slog"
	"math/rand"
	"os"
	"time"
)

func getTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func createTestGraph() []*graph.Edge {
	return []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "A", DestinationIp: "C", EdgeWeight: 2.0},
		{SourceIp: "B", DestinationIp: "C", EdgeWeight: 1.0},
		{SourceIp: "B", DestinationIp: "D", EdgeWeight: 3.0},
		{SourceIp: "C", DestinationIp: "D", EdgeWeight: 1.0},
		{SourceIp: "C", DestinationIp: "E", EdgeWeight: 2.0},
		{SourceIp: "D", DestinationIp: "E", EdgeWeight: 1.0},
		{SourceIp: "D", DestinationIp: "F", EdgeWeight: 2.0},
		{SourceIp: "E", DestinationIp: "F", EdgeWeight: 1.0},
	}
}

// GetRandomUtil generates random CPU utilization based on weighted distribution
func GetRandomUtil() int {
	rand.Seed(time.Now().UnixNano())
	type bin struct {
		low, high int
		weight    float64
	}
	bins := []bin{
		{30, 40, 71.21},
		{40, 50, 17.47},
		{50, 60, 8.82},
		{60, 70, 0.67},
		{70, 80, 1.33},
		{80, 90, 0.33},
		{90, 100, 0.17},
	}
	totalWeight := 0.0
	for _, item := range bins {
		totalWeight += item.weight
	}
	randVal := rand.Float64() * totalWeight
	sum := 0.0
	for _, item := range bins {
		sum += item.weight
		if randVal < sum {
			return rand.Intn(item.high-item.low) + item.low
		}
	}
	return 30
}
