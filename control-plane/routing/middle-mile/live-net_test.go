package middle_mile

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
		t.Logf("Path %d: %v", i, path.Hops)
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
