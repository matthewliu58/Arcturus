package middle_mile

import (
	"control-plane/routing/graph"
	"testing"
)

func TestONEWANSolver(t *testing.T) {
	edges := createTestGraph()
	solver := NewONEWANSolver(edges, 5)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "F", "test", logger)
	if err != nil {
		t.Fatalf("ONEWANSolver.Computing failed: %v", err)
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
		t.Logf("ONEWANSolver path %d: %v", i, path.Hops)
	}
}

func TestONEWANSolverMaxPaths(t *testing.T) {
	edges := createTestGraph()
	solver := NewONEWANSolver(edges, 3)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "F", "test", logger)
	if err != nil {
		t.Fatalf("ONEWANSolver.Computing failed: %v", err)
	}

	if len(paths) > 3 {
		t.Fatalf("Expected at most 3 paths, got %d", len(paths))
	}

	t.Logf("Found %d paths (max 3)", len(paths))
}

func TestONEWANSolverNoPath(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
	}
	solver := NewONEWANSolver(edges, 5)
	logger := getTestLogger()

	_, err := solver.Computing("A", "Z", "test", logger)
	if err == nil {
		t.Fatal("Expected error for non-existent path")
	}
}

func TestONEWANSolverDiversity(t *testing.T) {
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
	solver := NewONEWANSolver(edges, 5)
	logger := getTestLogger()

	paths, err := solver.Computing("S", "D", "test", logger)
	if err != nil {
		t.Fatalf("ONEWANSolver.Computing failed: %v", err)
	}

	if len(paths) == 0 {
		t.Fatal("Expected at least one path, got none")
	}

	t.Logf("Found %d diverse paths from S to D", len(paths))
	for i, path := range paths {
		t.Logf("Path %d: %v", i, path.Hops)
	}
}

func TestDinicGraph(t *testing.T) {
	g := NewDinicGraph()
	g.addEdge("s", "a", 1.0, 1.0)
	g.addEdge("s", "b", 1.0, 1.0)
	g.addEdge("a", "b", 1.0, 1.0)
	g.addEdge("a", "t", 1.0, 1.0)
	g.addEdge("b", "t", 1.0, 1.0)

	flow := g.maxFlow("s", "t")
	if flow < 0 {
		t.Fatalf("Expected non-negative flow, got %f", flow)
	}
	t.Logf("Max flow from s to t: %f", flow)
}
