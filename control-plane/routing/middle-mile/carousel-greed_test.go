package middle_mile

import (
	"control-plane/routing/graph"
	"testing"
)

func TestHeuristicSolver(t *testing.T) {
	edges := createTestGraph()
	solver := NewHeuristicSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "F", "test", logger)
	if err != nil {
		t.Fatalf("HeuristicSolver.Computing failed: %v", err)
	}

	if len(paths) == 0 {
		t.Fatal("Expected at least one path, got none")
	}

	for i, path := range paths {
		if len(path.Hops) == 0 {
			t.Fatalf("Path %d has no hops", i)
		}
		if path.Hops[0] != "A" || path.Hops[len(path.Hops)-1] != "F" {
			t.Fatalf("Path %d: expected from A to F, got %v", i, path.Hops)
		}
		t.Logf("HeuristicSolver path %d: %v", i, path.Hops)
	}
}

func TestHeuristicSolverNoPath(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
	}
	solver := NewHeuristicSolver(edges)
	logger := getTestLogger()

	_, err := solver.Computing("A", "Z", "test", logger)
	if err == nil {
		t.Fatal("Expected error for non-existent path")
	}
}

func TestHeuristicSolverSingleNode(t *testing.T) {
	edges := []*graph.Edge{}
	solver := NewHeuristicSolver(edges)
	logger := getTestLogger()

	_, err := solver.Computing("A", "A", "test", logger)
	if err == nil {
		t.Fatal("Expected error for same start and end")
	}
}

func TestHeuristicSolverMultiplePaths(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "S", DestinationIp: "A", EdgeWeight: 1.0},
		{SourceIp: "S", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "A", DestinationIp: "E", EdgeWeight: 1.0},
		{SourceIp: "B", DestinationIp: "E", EdgeWeight: 1.0},
		{SourceIp: "A", DestinationIp: "C", EdgeWeight: 1.0},
		{SourceIp: "B", DestinationIp: "D", EdgeWeight: 1.0},
		{SourceIp: "C", DestinationIp: "E", EdgeWeight: 1.0},
		{SourceIp: "D", DestinationIp: "E", EdgeWeight: 1.0},
	}
	solver := NewHeuristicSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("S", "E", "test", logger)
	if err != nil {
		t.Fatalf("HeuristicSolver.Computing failed: %v", err)
	}

	if len(paths) < 2 {
		t.Logf("Expected multiple paths, got %d", len(paths))
	}

	t.Logf("Found %d paths from S to E", len(paths))
	for i, path := range paths {
		t.Logf("Path %d: %v", i, path.Hops)
	}
}
