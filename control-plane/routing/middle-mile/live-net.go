package middle_mile

import (
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
	"sort"
)

type LiveNetSolver struct {
	edges []*graph.Edge
	alpha float64
	k     int
}

func NewLiveNetSolver(edges []*graph.Edge, k int) *LiveNetSolver {
	var g []*graph.Edge
	for _, e := range edges {
		g = append(g, e)
	}
	return &LiveNetSolver{
		edges: g,
		alpha: 1.2,
		k:     k,
	}
}

func (ls *LiveNetSolver) Computing(start, end, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
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

	// 1. Generate K shortest paths
	kspSolver := NewKShortestSolver(ls.edges, 5) // Generate 5 candidates
	candidatePaths, err := kspSolver.Computing(start, end, pre, logger)
	if err != nil {
		return nil, err
	}

	if len(candidatePaths) == 0 {
		return nil, fmt.Errorf("no paths found from %s to %s", start, end)
	}

	// 2. Calculate similarity for each path
	type pathWithSimilarity struct {
		path       routing.PathInfo
		similarity float64
	}

	var pathsWithSimilarity []pathWithSimilarity
	for _, path := range candidatePaths {
		sim := ls.averageSimilarity(path, candidatePaths)
		pathsWithSimilarity = append(pathsWithSimilarity, pathWithSimilarity{
			path:       path,
			similarity: sim,
		})
	}

	// 3. Sort by similarity ascending (more unique paths first)
	sort.Slice(pathsWithSimilarity, func(i, j int) bool {
		return pathsWithSimilarity[i].similarity < pathsWithSimilarity[j].similarity
	})

	// 4. Select top-K most unique paths
	var selectedPaths []routing.PathInfo
	count := 0
	for _, pws := range pathsWithSimilarity {
		if count >= ls.k {
			break
		}
		selectedPaths = append(selectedPaths, pws.path)
		count++
	}

	logger.Info("LiveNet paths selected", slog.String("pre", pre),
		slog.String("start", start), slog.String("end", end),
		slog.Int("k", ls.k), slog.Any("paths", selectedPaths))

	return selectedPaths, nil
}

// averageSimilarity calculates the average similarity between a path and all other paths
func (ls *LiveNetSolver) averageSimilarity(path routing.PathInfo, allPaths []routing.PathInfo) float64 {
	totalSimilarity := 0.0
	count := 0

	for _, otherPath := range allPaths {
		if path.Hops[0] == otherPath.Hops[0] && path.Hops[len(path.Hops)-1] == otherPath.Hops[len(otherPath.Hops)-1] {
			sim := ls.calculatePathSimilarity(path, otherPath)
			totalSimilarity += sim
			count++
		}
	}

	if count == 0 {
		return 1.0 // Maximum similarity if no comparison paths
	}

	return totalSimilarity / float64(count)
}

// calculatePathSimilarity calculates the similarity between two paths
// Similarity is based on the number of common nodes
func (ls *LiveNetSolver) calculatePathSimilarity(path1, path2 routing.PathInfo) float64 {
	// Create a set of nodes for path1
	nodeSet := make(map[string]bool)
	for _, node := range path1.Hops {
		nodeSet[node] = true
	}

	// Count common nodes
	commonCount := 0
	for _, node := range path2.Hops {
		if nodeSet[node] {
			commonCount++
		}
	}

	// Calculate Jaccard similarity: |A ∩ B| / |A ∪ B|
	totalNodes := len(nodeSet)
	for _, node := range path2.Hops {
		if !nodeSet[node] {
			totalNodes++
		}
	}

	if totalNodes == 0 {
		return 0.0
	}

	return float64(commonCount) / float64(totalNodes)
}
