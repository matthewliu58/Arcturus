package core_domain

import (
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
	"strings"
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
	logger.Info("LiveStyle: Starting KSP search", slog.String("pre", pre), slog.String("start", start), slog.String("end", end))
	kspSolver := NewKShortestSolver(ls.edges, 5)
	paths, err := kspSolver.yensAlgorithm(start, end, graph_, logger)
	if err != nil {
		return nil, err
	}

	logger.Info("LiveStyle: KSP search completed", slog.String("pre", pre), slog.String("start", start),
		slog.String("end", end), slog.Int("path_count", len(paths)))
	for i, p := range paths {
		hopStr := strings.Join(p.hops, "->")
		logger.Debug("LiveStyle: KSP path", slog.String("pre", pre), slog.String("start", start),
			slog.String("end", end), slog.Int("index", i+1), slog.Float64("cost", p.cost),
			slog.Float64("rawRTT", p.rawRTT), slog.Int("hops", len(p.hops)-1), slog.String("path", hopStr))
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
	logger.Info("LiveStyle: Selected paths", slog.String("pre", pre), slog.String("start", start),
		slog.String("end", end), slog.Int("k", ls.k), slog.Int("selected_count", len(selectedPaths)))
	for i, p := range selectedPaths {
		hopStr := strings.Join(p.hops, "->")
		logger.Info("LiveStyle: Selected path", slog.String("pre", pre), slog.String("start", start),
			slog.String("end", end), slog.Int("index", i+1), slog.Float64("cost", p.cost),
			slog.Float64("rawRTT", p.rawRTT), slog.Int("hops", len(p.hops)-1), slog.String("path", hopStr))
	}

	return pathInfos, nil
}

// selectTopPaths selects paths based on:
// 1. Filter out paths with >10 hops
// 2. Sort remaining paths by raw RTT (ascending)
// 3. Select top 2 paths
func (ls *LiveStyleSolver) selectTopPaths(paths []Path, count int) []Path {
	// Filter out paths with >10 hops
	var validPaths []Path
	for _, path := range paths {
		if len(path.hops)-1 <= 10 {
			validPaths = append(validPaths, path)
		}
	}

	// Sort by rawRTT (real latency) ascending
	for i := 0; i < len(validPaths)-1; i++ {
		for j := i + 1; j < len(validPaths); j++ {
			if validPaths[j].rawRTT < validPaths[i].rawRTT {
				validPaths[i], validPaths[j] = validPaths[j], validPaths[i]
			}
		}
	}

	// Select top count paths
	if len(validPaths) > count {
		return validPaths[:count]
	}
	return validPaths
}
