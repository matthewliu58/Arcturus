package core_domain

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

	// Verify RTT calculation: shortest path A->C->D->E->F = 2+1+1+1 = 5
	// But algorithm uses alpha=1.2, so RTT = 5 * 1.2 = 6.0
	expectedRtt := 6.0
	if paths[0].Rtt != expectedRtt {
		t.Errorf("Expected RTT %f, got %f", expectedRtt, paths[0].Rtt)
	}

	t.Logf("Dijkstra found path: %v with RTT: %f", paths[0].Hops, paths[0].Rtt)
}

func TestDijkstraSolverDirectPath(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "A", DestinationIp: "C", EdgeWeight: 2.0},
		{SourceIp: "B", DestinationIp: "C", EdgeWeight: 1.0},
	}
	solver := NewDijkstraSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "B", "test", logger)
	if err != nil {
		t.Fatalf("DijkstraSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("Expected 1 path, got %d", len(paths))
	}

	if len(paths[0].Hops) != 2 {
		t.Errorf("Expected direct path [A B], got %v", paths[0].Hops)
	}

	// RTT = 1.0 * 1.2 = 1.2
	expectedRtt := 1.2
	if paths[0].Rtt != expectedRtt {
		t.Errorf("Expected RTT %f, got %f", expectedRtt, paths[0].Rtt)
	}
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

func TestDijkstraSolverInvalidEnd(t *testing.T) {
	edges := createTestGraph()
	solver := NewDijkstraSolver(edges)
	logger := getTestLogger()

	_, err := solver.Computing("A", "Z", "test", logger)
	if err == nil {
		t.Fatal("Expected error for invalid end node")
	}
}

func TestDijkstraSolverSameStartEnd(t *testing.T) {
	edges := createTestGraph()
	solver := NewDijkstraSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "A", "test", logger)
	if err != nil {
		t.Fatalf("Expected path from A to A, got error: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("Expected 1 path, got %d", len(paths))
	}

	if len(paths[0].Hops) != 1 || paths[0].Hops[0] != "A" {
		t.Errorf("Expected [A], got %v", paths[0].Hops)
	}
}

func TestDijkstraSolverOrphanNode(t *testing.T) {
	// Graph with an orphan node that has no outgoing edges
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "C", DestinationIp: "D", EdgeWeight: 1.0}, // Orphan chain
	}
	solver := NewDijkstraSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "B", "test", logger)
	if err != nil {
		t.Fatalf("DijkstraSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("Expected 1 path, got %d", len(paths))
	}

	t.Logf("Orphan node test passed: path = %v", paths[0].Hops)
}

func TestDijkstraSolverZeroWeight(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 0.0},
		{SourceIp: "B", DestinationIp: "C", EdgeWeight: 1.0},
	}
	solver := NewDijkstraSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "C", "test", logger)
	if err != nil {
		t.Fatalf("DijkstraSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("Expected 1 path, got %d", len(paths))
	}

	// RTT = (0 + 1) * 1.2 = 1.2
	expectedRtt := 1.2
	if paths[0].Rtt != expectedRtt {
		t.Errorf("Expected RTT %f, got %f", expectedRtt, paths[0].Rtt)
	}
}

func TestDijkstraSolverOptimalPath(t *testing.T) {
	// Test path with more hops but potentially lower RTT due to weights
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 10.0},
		{SourceIp: "A", DestinationIp: "C", EdgeWeight: 1.0},
		{SourceIp: "C", DestinationIp: "D", EdgeWeight: 1.0},
		{SourceIp: "D", DestinationIp: "E", EdgeWeight: 1.0},
		{SourceIp: "E", DestinationIp: "B", EdgeWeight: 1.0},
	}
	solver := NewDijkstraSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "B", "test", logger)
	if err != nil {
		t.Fatalf("DijkstraSolver.Computing failed: %v", err)
	}

	if len(paths) == 0 {
		t.Fatal("Expected at least one path")
	}

	// Optimal path should be A->C->D->E->B with weight 4.0, RTT = 4.8
	// Not A->B with weight 10.0, RTT = 12.0
	if len(paths[0].Hops) != 5 {
		t.Errorf("Expected 5-hop path, got %v", paths[0].Hops)
	}

	// RTT should be 4 * 1.2 = 4.8
	expectedRtt := 4.8
	if paths[0].Rtt != expectedRtt {
		t.Errorf("Expected RTT %f, got %f", expectedRtt, paths[0].Rtt)
	}

	t.Logf("Optimal path test: %v, RTT: %f", paths[0].Hops, paths[0].Rtt)
}

func TestDijkstraSolverSelfLoop(t *testing.T) {
	// Graph with self-loop (should be ignored)
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "A", EdgeWeight: 1.0}, // Self-loop
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 5.0},
	}
	solver := NewDijkstraSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "B", "test", logger)
	if err != nil {
		t.Fatalf("DijkstraSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("Expected 1 path, got %d", len(paths))
	}

	// Should ignore self-loop and use A->B directly
	if len(paths[0].Hops) != 2 {
		t.Errorf("Expected 2-hop path [A B], got %v", paths[0].Hops)
	}
}

func TestDijkstraSolverEmptyGraph(t *testing.T) {
	edges := []*graph.Edge{}
	solver := NewDijkstraSolver(edges)
	logger := getTestLogger()

	_, err := solver.Computing("A", "B", "test", logger)
	if err == nil {
		t.Fatal("Expected error for empty graph")
	}
}

func TestDijkstraSolverNodeNoOutgoingEdges(t *testing.T) {
	// Node exists but has no outgoing edges (dangling node)
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "C", DestinationIp: "D", EdgeWeight: 1.0}, // C has outgoing but no incoming
	}
	solver := NewDijkstraSolver(edges)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "B", "test", logger)
	if err != nil {
		t.Fatalf("DijkstraSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 || len(paths[0].Hops) != 2 {
		t.Errorf("Expected path [A B], got %v", paths)
	}
}
