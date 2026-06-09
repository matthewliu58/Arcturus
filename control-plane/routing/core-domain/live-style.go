package core_domain

import (
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
)

// LiveStyleSolver implements a path selection algorithm that:
// 1. Generates K shortest paths using Yen's algorithm
// 2. Selects top paths based on hop count priority and RTT
//   - Prioritize paths with <=4 hops
//   - Sort by raw RTT within each hop category
type LiveStyleSolver struct {
	edges []*graph.Edge
	k     int
}

func NewLiveStyleSolver(edges []*graph.Edge, k int) *LiveStyleSolver {
	var g []*graph.Edge
	for _, e := range edges {
		g = append(g, e)
	}
	if k <= 0 {
		k = 2
	}
	return &LiveStyleSolver{
		edges: g,
		k:     k,
	}
}

func (ls *LiveStyleSolver) Computing(start, end, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	// Build graph structure
	graph_ := make(map[string][]*graph.Edge)
	nodes := make(map[string]struct{})
	for _, e := range ls.edges {
		graph_[e.SourceIp] = append(graph_[e.SourceIp], e)
		nodes[e.SourceIp] = struct{}{}
		nodes[e.DestinationIp] = struct{}{}
	}

	// Check if start and end nodes exist
	if _, ok := nodes[start]; !ok {
		return nil, fmt.Errorf("start node %s not found", start)
	}
	if _, ok := nodes[end]; !ok {
		return nil, fmt.Errorf("end node %s not found", end)
	}

	// Step 1: Generate K=5 shortest paths using Yen's algorithm
	kspSolver := NewKShortestSolver(ls.edges, 5)
	paths, err := kspSolver.yensAlgorithm(start, end, graph_, logger)
	if err != nil {
		return nil, err
	}

	// Step 2: Select top paths based on hop count and RTT
	selectedPaths := ls.selectTopPaths(paths, ls.k)

	// Step 3: Convert to PathInfo format
	var pathInfos []routing.PathInfo
	for _, path := range selectedPaths {
		pathInfos = append(pathInfos, routing.PathInfo{
			Hops:   path.hops,
			Rtt:    path.cost,
			RawRTT: path.rawRTT,
		})
	}

	logger.Info("LiveStyle paths selected", slog.String("pre", pre),
		slog.String("start", start), slog.String("end", end),
		slog.Int("k", ls.k), slog.Int("selected", len(pathInfos)),
		slog.Any("paths", pathInfos))

	return pathInfos, nil
}

// selectTopPaths selects paths based on:
// 1. First prioritize paths with <=4 hops
// 2. Within each hop category, sort by raw RTT (ascending)
func (ls *LiveStyleSolver) selectTopPaths(paths []Path, count int) []Path {
	if len(paths) <= count {
		return paths
	}

	// Separate paths into two groups
	var shortPaths []Path // hops <= 4
	var longPaths []Path  // hops > 4

	for _, path := range paths {
		if len(path.hops)-1 <= 4 {
			shortPaths = append(shortPaths, path)
		} else {
			longPaths = append(longPaths, path)
		}
	}

	// Sort short paths by rawRTT (real latency)
	for i := 0; i < len(shortPaths)-1; i++ {
		for j := i + 1; j < len(shortPaths); j++ {
			if shortPaths[j].rawRTT < shortPaths[i].rawRTT {
				shortPaths[i], shortPaths[j] = shortPaths[j], shortPaths[i]
			}
		}
	}

	// Sort long paths by rawRTT (real latency)
	for i := 0; i < len(longPaths)-1; i++ {
		for j := i + 1; j < len(longPaths); j++ {
			if longPaths[j].rawRTT < longPaths[i].rawRTT {
				longPaths[i], longPaths[j] = longPaths[j], longPaths[i]
			}
		}
	}

	// Select paths: prioritize short paths, then long paths
	var result []Path
	remaining := count

	// Add short paths first
	for i := 0; i < len(shortPaths) && remaining > 0; i++ {
		result = append(result, shortPaths[i])
		remaining--
	}

	// Add long paths if needed
	for i := 0; i < len(longPaths) && remaining > 0; i++ {
		result = append(result, longPaths[i])
		remaining--
	}

	return result
}
