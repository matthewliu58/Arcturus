package core_domain

import (
	"control-plane/routing/graph"
	"log/slog"
	"os"
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
