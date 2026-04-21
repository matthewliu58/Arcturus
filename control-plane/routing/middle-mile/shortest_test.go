package middle_mile

import (
	"control-plane/routing/graph"
	"testing"
)

func TestDijkstraSolver(t *testing.T) {
	edges := createTestGraph()
	solver := NewDijkstraSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "F", "test", logger)
	if err != nil {
		t.Fatalf("DijkstraSolver.Computing failed: %v", err)
	}

	if len(paths) == 0 {
		t.Fatal("Expected at least one path, got none")
	}

	if len(paths[0].Hops) == 0 {
		t.Fatal("Expected path to have hops")
	}

	if paths[0].Hops[0] != "A" || paths[0].Hops[len(paths[0].Hops)-1] != "F" {
		t.Fatalf("Expected path from A to F, got %v", paths[0].Hops)
	}

	t.Logf("Dijkstra found path: %v with RTT: %f", paths[0].Hops, paths[0].Rtt)
}

func TestDijkstraSolverNoPath(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
	}
	solver := NewDijkstraSolver(edges)
	logger := getTestLogger()

	_, err := solver.Computing("A", "Z", "test", logger)
	if err == nil {
		t.Fatal("Expected error for non-existent path")
	}
}

func TestDijkstraSolverInvalidStart(t *testing.T) {
	edges := createTestGraph()
	solver := NewDijkstraSolver(edges)
	logger := getTestLogger()

	_, err := solver.Computing("Z", "F", "test", logger)
	if err == nil {
		t.Fatal("Expected error for invalid start node")
	}
}
