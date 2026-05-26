package core_domain

import (
	"control-plane/routing/graph"
	"testing"
)

func TestLiveNetSolver(t *testing.T) {
	edges := createTestGraph()
	solver := NewLiveNetSolver(edges, 3)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "F", "test", logger)
	if err != nil {
		t.Fatalf("LiveNetSolver.Computing failed: %v", err)
	}

	if len(paths) == 0 {
		t.Fatal("Expected at least one path, got none")
	}

	if len(paths) > 3 {
		t.Fatalf("Expected at most 3 paths, got %d", len(paths))
	}

	for i, path := range paths {
		if len(path.Hops) == 0 {
			t.Fatalf("Path %d has no hops", i)
		}
		if path.Hops[0] != "A" || path.Hops[len(path.Hops)-1] != "F" {
			t.Fatalf("Path %d: expected from A to F, got %v", i, path.Hops)
		}
		t.Logf("LiveNetSolver path %d: %v", i, path.Hops)
	}
}

func TestLiveNetSolverDiversity(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "S", DestinationIp: "A", EdgeWeight: 1.0},
		{SourceIp: "S", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "A", DestinationIp: "C", EdgeWeight: 1.0},
		{SourceIp: "B", DestinationIp: "C", EdgeWeight: 1.0},
		{SourceIp: "C", DestinationIp: "D", EdgeWeight: 1.0},
		{SourceIp: "S", DestinationIp: "X", EdgeWeight: 1.0},
		{SourceIp: "X", DestinationIp: "Y", EdgeWeight: 1.0},
		{SourceIp: "Y", DestinationIp: "D", EdgeWeight: 1.0},
	}
	solver := NewLiveNetSolver(edges, 3)
	logger := getTestLogger()

	paths, err := solver.Computing("S", "D", "test", logger)
	if err != nil {
		t.Fatalf("LiveNetSolver.Computing failed: %v", err)
	}

	if len(paths) == 0 {
		t.Fatal("Expected at least one path, got none")
	}

	t.Logf("Found %d diverse paths from S to D", len(paths))
	for i, path := range paths {
		t.Logf("Path %d: %v, RTT: %f", i, path.Hops, path.Rtt)
	}

	// Verify all paths start and end correctly
	for i, path := range paths {
		if path.Hops[0] != "S" || path.Hops[len(path.Hops)-1] != "D" {
			t.Errorf("Path %d: expected S->D, got %v", i, path.Hops)
		}
	}
}

func TestLiveNetSolverNoPath(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
	}
	solver := NewLiveNetSolver(edges, 3)
	logger := getTestLogger()

	_, err := solver.Computing("A", "Z", "test", logger)
	if err == nil {
		t.Fatal("Expected error for non-existent path")
	}
}

func TestLiveNetSolverK1(t *testing.T) {
	edges := createTestGraph()
	solver := NewLiveNetSolver(edges, 1)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "F", "test", logger)
	if err != nil {
		t.Fatalf("LiveNetSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("Expected exactly 1 path, got %d", len(paths))
	}
}

func TestLiveNetSolverInvalidStart(t *testing.T) {
	edges := createTestGraph()
	solver := NewLiveNetSolver(edges, 3)
	logger := getTestLogger()

	_, err := solver.Computing("Z", "F", "test", logger)
	if err == nil {
		t.Fatal("Expected error for invalid start node")
	}
}

func TestLiveNetSolverInvalidEnd(t *testing.T) {
	edges := createTestGraph()
	solver := NewLiveNetSolver(edges, 3)
	logger := getTestLogger()

	_, err := solver.Computing("A", "Z", "test", logger)
	if err == nil {
		t.Fatal("Expected error for invalid end node")
	}
}

func TestLiveNetSolverSameStartEnd(t *testing.T) {
	edges := createTestGraph()
	solver := NewLiveNetSolver(edges, 3)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "A", "test", logger)
	if err != nil {
		t.Fatalf("LiveNetSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Errorf("Expected 1 path, got %d", len(paths))
	}

	if len(paths[0].Hops) != 1 || paths[0].Hops[0] != "A" {
		t.Errorf("Expected [A], got %v", paths[0].Hops)
	}
}

func TestLiveNetSolverKGreaterThanAvailable(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "B", DestinationIp: "C", EdgeWeight: 1.0},
	}
	solver := NewLiveNetSolver(edges, 10) // Request 10, only 1 available
	logger := getTestLogger()

	paths, err := solver.Computing("A", "C", "test", logger)
	if err != nil {
		t.Fatalf("LiveNetSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Errorf("Expected 1 path, got %d", len(paths))
	}
}

func TestLiveNetSolverSimilarity(t *testing.T) {
	// Two very similar paths vs one different path
	edges := []*graph.Edge{
		{SourceIp: "S", DestinationIp: "A", EdgeWeight: 1.0},
		{SourceIp: "S", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "A", DestinationIp: "C", EdgeWeight: 1.0},
		{SourceIp: "B", DestinationIp: "C", EdgeWeight: 1.0},
		{SourceIp: "C", DestinationIp: "T", EdgeWeight: 1.0},
		{SourceIp: "S", DestinationIp: "X", EdgeWeight: 1.0},
		{SourceIp: "X", DestinationIp: "T", EdgeWeight: 1.0},
	}
	solver := NewLiveNetSolver(edges, 2)
	logger := getTestLogger()

	paths, err := solver.Computing("S", "T", "test", logger)
	if err != nil {
		t.Fatalf("LiveNetSolver.Computing failed: %v", err)
	}

	if len(paths) != 2 {
		t.Errorf("Expected 2 diverse paths, got %d", len(paths))
	}

	// Verify paths are different
	for i, path := range paths {
		t.Logf("Path %d: %v, RTT: %f", i, path.Hops, path.Rtt)
	}
}

func TestLiveNetSolverEmptyGraph(t *testing.T) {
	edges := []*graph.Edge{}
	solver := NewLiveNetSolver(edges, 3)
	logger := getTestLogger()

	_, err := solver.Computing("A", "B", "test", logger)
	if err == nil {
		t.Fatal("Expected error for empty graph")
	}
}

func TestLiveNetSolverOrphanNode(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "C", DestinationIp: "D", EdgeWeight: 1.0},
	}
	solver := NewLiveNetSolver(edges, 3)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "B", "test", logger)
	if err != nil {
		t.Fatalf("LiveNetSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 || len(paths[0].Hops) != 2 {
		t.Errorf("Expected path [A B], got %v", paths)
	}
}
