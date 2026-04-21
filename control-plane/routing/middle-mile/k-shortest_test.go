package middle_mile

import (
	"control-plane/routing/graph"
	"testing"
)

func TestKShortestSolver(t *testing.T) {
	edges := createTestGraph()
	solver := NewKShortestSolver(edges, 3)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "F", "test", logger)
	if err != nil {
		t.Fatalf("KShortestSolver.Computing failed: %v", err)
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
		t.Logf("KShortestSolver path %d: %v, RTT: %f", i, path.Hops, path.Rtt)
	}
}

func TestKShortestSolverK1(t *testing.T) {
	edges := createTestGraph()
	solver := NewKShortestSolver(edges, 1)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "F", "test", logger)
	if err != nil {
		t.Fatalf("KShortestSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("Expected exactly 1 path, got %d", len(paths))
	}
}

func TestKShortestSolverNoPath(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
	}
	solver := NewKShortestSolver(edges, 3)
	logger := getTestLogger()

	_, err := solver.Computing("A", "Z", "test", logger)
	if err == nil {
		t.Fatal("Expected error for non-existent path")
	}
}

func TestKShortestSolverPathsOrder(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "S", DestinationIp: "A", EdgeWeight: 1.0},
		{SourceIp: "S", DestinationIp: "B", EdgeWeight: 2.0},
		{SourceIp: "A", DestinationIp: "E", EdgeWeight: 1.0},
		{SourceIp: "B", DestinationIp: "E", EdgeWeight: 1.0},
		{SourceIp: "S", DestinationIp: "C", EdgeWeight: 3.0},
		{SourceIp: "C", DestinationIp: "E", EdgeWeight: 1.0},
	}
	solver := NewKShortestSolver(edges, 3)
	logger := getTestLogger()

	paths, err := solver.Computing("S", "E", "test", logger)
	if err != nil {
		t.Fatalf("KShortestSolver.Computing failed: %v", err)
	}

	if len(paths) == 0 {
		t.Fatal("Expected at least one path, got none")
	}

	for i := 1; i < len(paths); i++ {
		if paths[i].Rtt < paths[i-1].Rtt {
			t.Errorf("Paths not in order: path %d RTT %f < path %d RTT %f",
				i, paths[i].Rtt, i-1, paths[i-1].Rtt)
		}
	}

	t.Logf("Found %d paths, ordered by RTT", len(paths))
}
