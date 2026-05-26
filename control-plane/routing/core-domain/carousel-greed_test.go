package core_domain

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
		t.Fatal("Expected error for same start and end with empty graph")
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

	if len(paths) < 1 {
		t.Errorf("Expected at least one path, got %d", len(paths))
	}

	t.Logf("Found %d paths from S to E", len(paths))
	for i, path := range paths {
		t.Logf("Path %d: %v, RTT: %f", i, path.Hops, path.Rtt)
	}
}

func TestHeuristicSolverInvalidStart(t *testing.T) {
	edges := createTestGraph()
	solver := NewHeuristicSolver(edges)
	logger := getTestLogger()

	_, err := solver.Computing("Z", "F", "test", logger)
	if err == nil {
		t.Fatal("Expected error for invalid start node")
	}
}

func TestHeuristicSolverInvalidEnd(t *testing.T) {
	edges := createTestGraph()
	solver := NewHeuristicSolver(edges)
	logger := getTestLogger()

	_, err := solver.Computing("A", "Z", "test", logger)
	if err == nil {
		t.Fatal("Expected error for invalid end node")
	}
}

func TestHeuristicSolverSameStartEnd(t *testing.T) {
	edges := createTestGraph()
	solver := NewHeuristicSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "A", "test", logger)
	if err != nil {
		t.Fatalf("HeuristicSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Errorf("Expected 1 path, got %d", len(paths))
	}

	if len(paths[0].Hops) != 1 || paths[0].Hops[0] != "A" {
		t.Errorf("Expected [A], got %v", paths[0].Hops)
	}
}

func TestHeuristicSolverDirectPath(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 5.0},
		{SourceIp: "A", DestinationIp: "C", EdgeWeight: 1.0},
		{SourceIp: "C", DestinationIp: "D", EdgeWeight: 1.0},
		{SourceIp: "D", DestinationIp: "B", EdgeWeight: 1.0},
	}
	solver := NewHeuristicSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "B", "test", logger)
	if err != nil {
		t.Fatalf("HeuristicSolver.Computing failed: %v", err)
	}

	if len(paths) == 0 {
		t.Fatal("Expected at least one path")
	}

	// Verify path exists and is valid (start and end correct)
	// Note: HeuristicSolver doesn't compute RTT, it returns 0
	if paths[0].Hops[0] != "A" || paths[0].Hops[len(paths[0].Hops)-1] != "B" {
		t.Errorf("Expected path from A to B, got %v", paths[0].Hops)
	}
}

func TestHeuristicSolverEmptyGraph(t *testing.T) {
	edges := []*graph.Edge{}
	solver := NewHeuristicSolver(edges)
	logger := getTestLogger()

	_, err := solver.Computing("A", "B", "test", logger)
	if err == nil {
		t.Fatal("Expected error for empty graph")
	}
}

func TestHeuristicSolverOrphanNode(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "C", DestinationIp: "D", EdgeWeight: 1.0}, // Orphan chain
	}
	solver := NewHeuristicSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "B", "test", logger)
	if err != nil {
		t.Fatalf("HeuristicSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 || len(paths[0].Hops) != 2 {
		t.Errorf("Expected path [A B], got %v", paths)
	}
}
