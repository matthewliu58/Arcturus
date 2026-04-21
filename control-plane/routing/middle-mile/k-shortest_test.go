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

	// Paths should be ordered by RTT (non-decreasing)
	for i := 1; i < len(paths); i++ {
		if paths[i].Rtt < paths[i-1].Rtt {
			t.Errorf("Paths not in order: path %d RTT %f < path %d RTT %f",
				i, paths[i].Rtt, i-1, paths[i-1].Rtt)
		}
	}

	// First path should be S->A->E with weight 2.0, RTT = 2.4
	expectedFirstRtt := 2.4
	if paths[0].Rtt != expectedFirstRtt {
		t.Errorf("Expected first path RTT %f, got %f", expectedFirstRtt, paths[0].Rtt)
	}

	t.Logf("Found %d paths, ordered by RTT", len(paths))
}

func TestKShortestSolverEqualRTTPaths(t *testing.T) {
	// Two paths with same RTT
	edges := []*graph.Edge{
		{SourceIp: "S", DestinationIp: "A", EdgeWeight: 1.0},
		{SourceIp: "S", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "A", DestinationIp: "E", EdgeWeight: 1.0},
		{SourceIp: "B", DestinationIp: "E", EdgeWeight: 1.0},
	}
	solver := NewKShortestSolver(edges, 3)
	logger := getTestLogger()

	paths, err := solver.Computing("S", "E", "test", logger)
	if err != nil {
		t.Fatalf("KShortestSolver.Computing failed: %v", err)
	}

	if len(paths) != 2 {
		t.Errorf("Expected 2 paths, got %d", len(paths))
	}

	// Both paths should have RTT = 2.4
	for i, path := range paths {
		if path.Rtt != 2.4 {
			t.Errorf("Path %d: expected RTT 2.4, got %f", i, path.Rtt)
		}
	}
}

func TestKShortestSolverSameStartEnd(t *testing.T) {
	edges := createTestGraph()
	solver := NewKShortestSolver(edges, 3)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "A", "test", logger)
	if err != nil {
		t.Fatalf("KShortestSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Errorf("Expected 1 path (self), got %d", len(paths))
	}

	if len(paths[0].Hops) != 1 || paths[0].Hops[0] != "A" {
		t.Errorf("Expected [A], got %v", paths[0].Hops)
	}
}

func TestKShortestSolverKGreaterThanAvailable(t *testing.T) {
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "B", DestinationIp: "C", EdgeWeight: 1.0},
	}
	solver := NewKShortestSolver(edges, 10) // Request 10, only 1 available
	logger := getTestLogger()

	paths, err := solver.Computing("A", "C", "test", logger)
	if err != nil {
		t.Fatalf("KShortestSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Errorf("Expected 1 path, got %d", len(paths))
	}
}

func TestKShortestSolverTriangle(t *testing.T) {
	// Classic triangle: A->B->C and A->C direct
	edges := []*graph.Edge{
		{SourceIp: "A", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "B", DestinationIp: "C", EdgeWeight: 1.0},
		{SourceIp: "A", DestinationIp: "C", EdgeWeight: 5.0},
	}
	solver := NewKShortestSolver(edges, 2)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "C", "test", logger)
	if err != nil {
		t.Fatalf("KShortestSolver.Computing failed: %v", err)
	}

	if len(paths) != 2 {
		t.Errorf("Expected 2 paths, got %d", len(paths))
	}

	// First should be A->B->C with RTT = 2.4
	if paths[0].Rtt != 2.4 {
		t.Errorf("First path: expected RTT 2.4, got %f", paths[0].Rtt)
	}

	// Second should be A->C with RTT = 6.0
	if paths[1].Rtt != 6.0 {
		t.Errorf("Second path: expected RTT 6.0, got %f", paths[1].Rtt)
	}
}

func TestKShortestSolverInvalidStart(t *testing.T) {
	edges := createTestGraph()
	solver := NewKShortestSolver(edges, 3)
	logger := getTestLogger()

	_, err := solver.Computing("Z", "F", "test", logger)
	if err == nil {
		t.Fatal("Expected error for invalid start node")
	}
}

func TestKShortestSolverInvalidEnd(t *testing.T) {
	edges := createTestGraph()
	solver := NewKShortestSolver(edges, 3)
	logger := getTestLogger()

	_, err := solver.Computing("A", "Z", "test", logger)
	if err == nil {
		t.Fatal("Expected error for invalid end node")
	}
}
