package core_domain

import (
	"control-plane/routing/graph"
	"control-plane/routing/routing"
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

	// Verify diversity: paths should have different intermediate nodes
	for i, path := range paths {
		t.Logf("Path %d: %v", i, path.Hops)
	}

	// If multiple paths exist, verify they are different
	if len(paths) > 1 {
		for i := 1; i < len(paths); i++ {
			if pathsEqual(paths[i], paths[i-1]) {
				t.Errorf("Path %d and %d are identical: %v", i-1, i, paths[i].Hops)
			}
		}
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
	// Max flow from s to t is 2 (s->a->t and s->b->t)
	expectedFlow := 2.0
	if flow != expectedFlow {
		t.Errorf("Expected flow %f, got %f", expectedFlow, flow)
	}
	t.Logf("Max flow from s to t: %f", flow)
}

func TestDinicGraphNoPath(t *testing.T) {
	g := NewDinicGraph()
	g.addEdge("a", "b", 1.0, 1.0)

	flow := g.maxFlow("a", "z") // z doesn't exist
	if flow != 0 {
		t.Errorf("Expected flow 0 for non-existent path, got %f", flow)
	}
}

func TestDinicGraphSingleEdge(t *testing.T) {
	g := NewDinicGraph()
	g.addEdge("s", "t", 5.0, 1.0)

	flow := g.maxFlow("s", "t")
	if flow != 5.0 {
		t.Errorf("Expected flow 5.0, got %f", flow)
	}
}

func TestDinicGraphMultiplePaths(t *testing.T) {
	// Build a graph with 3 parallel paths
	g := NewDinicGraph()
	g.addEdge("s", "a", 1.0, 1.0)
	g.addEdge("s", "b", 1.0, 1.0)
	g.addEdge("s", "c", 1.0, 1.0)
	g.addEdge("a", "t", 1.0, 1.0)
	g.addEdge("b", "t", 1.0, 1.0)
	g.addEdge("c", "t", 1.0, 1.0)

	flow := g.maxFlow("s", "t")
	expectedFlow := 3.0
	if flow != expectedFlow {
		t.Errorf("Expected flow %f, got %f", expectedFlow, flow)
	}
}

func TestDinicGraphChain(t *testing.T) {
	// Simple chain: s -> a -> b -> t
	g := NewDinicGraph()
	g.addEdge("s", "a", 1.0, 1.0)
	g.addEdge("a", "b", 1.0, 1.0)
	g.addEdge("b", "t", 1.0, 1.0)

	flow := g.maxFlow("s", "t")
	expectedFlow := 1.0
	if flow != expectedFlow {
		t.Errorf("Expected flow %f, got %f", expectedFlow, flow)
	}
}

func TestONEWANSolverInvalidStart(t *testing.T) {
	edges := createTestGraph()
	solver := NewONEWANSolver(edges, 5)
	logger := getTestLogger()

	_, err := solver.Computing("Z", "F", "test", logger)
	if err == nil {
		t.Fatal("Expected error for invalid start node")
	}
}

func TestONEWANSolverInvalidEnd(t *testing.T) {
	edges := createTestGraph()
	solver := NewONEWANSolver(edges, 5)
	logger := getTestLogger()

	_, err := solver.Computing("A", "Z", "test", logger)
	if err == nil {
		t.Fatal("Expected error for invalid end node")
	}
}

func TestONEWANSolverSameStartEnd(t *testing.T) {
	edges := createTestGraph()
	solver := NewONEWANSolver(edges, 5)
	logger := getTestLogger()

	paths, err := solver.Computing("A", "A", "test", logger)
	if err != nil {
		t.Fatalf("ONEWANSolver.Computing failed: %v", err)
	}

	if len(paths) != 1 {
		t.Errorf("Expected 1 path, got %d", len(paths))
	}

	if len(paths[0].Hops) != 1 || paths[0].Hops[0] != "A" {
		t.Errorf("Expected [A], got %v", paths[0].Hops)
	}
}

// --- ComputingMulti tests ---

func TestComputingMulti_Basic(t *testing.T) {
	edges := createTestGraph()
	solver := NewONEWANSolver(edges, 3) // 3 paths per destination
	logger := getTestLogger()

	ends := []string{"D", "F"}
	paths, err := solver.ComputingMulti("A", ends, "test", logger)
	if err != nil {
		t.Fatalf("ComputingMulti failed: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("Expected at least one path, got none")
	}
	// 2 destinations * 3 maxPaths = 6 max, but D and F may have fewer
	t.Logf("ComputingMulti A→%v got %d paths", ends, len(paths))
	for i, p := range paths {
		t.Logf("  path %d: %v (start=%s end=%s)", i, p.Hops, p.Hops[0], p.Hops[len(p.Hops)-1])
	}

	// All paths must start at A
	for i, p := range paths {
		if p.Hops[0] != "A" {
			t.Errorf("path %d does not start at A: %v", i, p.Hops)
		}
	}
}

func TestComputingMulti_SingleDest(t *testing.T) {
	edges := createTestGraph()
	solver := NewONEWANSolver(edges, 5)
	logger := getTestLogger()

	// Single destination should delegate to Computing and return same result
	multiPaths, err := solver.ComputingMulti("A", []string{"F"}, "test", logger)
	if err != nil {
		t.Fatalf("ComputingMulti single dest failed: %v", err)
	}
	singlePaths, err := solver.Computing("A", "F", "test", logger)
	if err != nil {
		t.Fatalf("Computing failed: %v", err)
	}
	if len(multiPaths) != len(singlePaths) {
		t.Errorf("ComputingMulti single dest got %d paths, Computing got %d", len(multiPaths), len(singlePaths))
	}
	t.Logf("multi=%d paths, single=%d paths", len(multiPaths), len(singlePaths))
}

func TestComputingMulti_EmptyEnds(t *testing.T) {
	edges := createTestGraph()
	solver := NewONEWANSolver(edges, 5)
	logger := getTestLogger()

	// Empty ends falls back to Computing(start, "") — "" is not a node, so expects error
	_, err := solver.ComputingMulti("A", nil, "test", logger)
	if err == nil {
		t.Fatal("Expected error when ends list is empty (fallback to empty end)")
	}
	t.Logf("Correctly errored on empty ends: %v", err)
}

func TestComputingMulti_SomeUnreachable(t *testing.T) {
	edges := createTestGraph()
	solver := NewONEWANSolver(edges, 3)
	logger := getTestLogger()

	// Z is unreachable, F is reachable → should return paths to F, skip Z
	ends := []string{"Z", "F"}
	paths, err := solver.ComputingMulti("A", ends, "test", logger)
	if err != nil {
		t.Fatalf("ComputingMulti with unreachable dest failed: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("Expected paths to reachable destination F, got none")
	}
	// All paths should end at F
	for i, p := range paths {
		last := p.Hops[len(p.Hops)-1]
		if last != "F" {
			t.Errorf("path %d ends at %s, expected F: %v", i, last, p.Hops)
		}
	}
	t.Logf("ComputingMulti A→[Z,F] got %d paths (all to F)", len(paths))
}

func TestComputingMulti_AllUnreachable(t *testing.T) {
	edges := createTestGraph()
	solver := NewONEWANSolver(edges, 3)
	logger := getTestLogger()

	// All destinations unreachable
	_, err := solver.ComputingMulti("A", []string{"Z", "Y"}, "test", logger)
	if err == nil {
		t.Fatal("Expected error when all destinations are unreachable")
	}
	t.Logf("Correctly errored: %v", err)
}

func TestComputingMulti_InvalidStart(t *testing.T) {
	edges := createTestGraph()
	solver := NewONEWANSolver(edges, 3)
	logger := getTestLogger()

	_, err := solver.ComputingMulti("Z", []string{"A", "B"}, "test", logger)
	if err == nil {
		t.Fatal("Expected error for invalid start node")
	}
	t.Logf("Correctly errored: %v", err)
}

func TestONEWANDiversityThreshold(t *testing.T) {
	// Two very different paths should both be selected
	edges := []*graph.Edge{
		{SourceIp: "S", DestinationIp: "A", EdgeWeight: 1.0},
		{SourceIp: "S", DestinationIp: "B", EdgeWeight: 1.0},
		{SourceIp: "A", DestinationIp: "T", EdgeWeight: 1.0},
		{SourceIp: "B", DestinationIp: "T", EdgeWeight: 1.0},
		{SourceIp: "S", DestinationIp: "C", EdgeWeight: 1.0},
		{SourceIp: "C", DestinationIp: "T", EdgeWeight: 1.0},
	}
	solver := NewONEWANSolver(edges, 5)
	logger := getTestLogger()

	paths, err := solver.Computing("S", "T", "test", logger)
	if err != nil {
		t.Fatalf("ONEWANSolver.Computing failed: %v", err)
	}

	if len(paths) < 2 {
		t.Errorf("Expected at least 2 diverse paths, got %d", len(paths))
	}
}

// Helper function
func pathsEqual(p1, p2 routing.PathInfo) bool {
	if len(p1.Hops) != len(p2.Hops) {
		return false
	}
	for i := range p1.Hops {
		if p1.Hops[i] != p2.Hops[i] {
			return false
		}
	}
	return true
}
