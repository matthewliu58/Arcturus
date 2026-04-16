package middle_mile

import (
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
	"math"
	"sort"
)

type ONEWANSolver struct {
	edges    []*graph.Edge
	alpha    float64
	maxPaths int
}

func NewONEWANSolver(edges []*graph.Edge, maxPaths int) *ONEWANSolver {
	var g []*graph.Edge
	for _, e := range edges {
		g = append(g, e)
	}
	return &ONEWANSolver{
		edges:    g,
		alpha:    1.2,
		maxPaths: maxPaths,
	}
}

func (os *ONEWANSolver) Computing(start, end, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	// Build graph structure
	graph_ := make(map[string][]*graph.Edge)
	nodes := make(map[string]struct{})
	for _, e := range os.edges {
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

	// 1. Generate candidate paths using max flow approach
	candidates, err := os.maxFlowPaths(start, end, graph_, 10, logger)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no paths found from %s to %s", start, end)
	}

	// 2. Apply diversity filter
	filtered := os.diversityFilter(candidates)

	// 3. Select all filtered paths (focus on path count)
	var selectedPaths []routing.PathInfo
	for _, path := range filtered {
		selectedPaths = append(selectedPaths, path)
	}

	logger.Info("ONEWAN paths selected", slog.String("pre", pre),
		slog.String("start", start), slog.String("end", end),
		slog.Int("count", len(selectedPaths)), slog.Any("paths", selectedPaths))

	return selectedPaths, nil
}

// Dinic's algorithm implementation for max flow

type Edge struct {
	target   string
	rev      int
	capacity float64
	weight   float64
}

type DinicGraph struct {
	adj map[string][]Edge
}

func NewDinicGraph() *DinicGraph {
	return &DinicGraph{
		adj: make(map[string][]Edge),
	}
}

func (g *DinicGraph) addEdge(from, to string, capacity, weight float64) {
	// Forward edge
	g.adj[from] = append(g.adj[from], Edge{to, len(g.adj[to]), capacity, weight})
	// Backward edge
	g.adj[to] = append(g.adj[to], Edge{from, len(g.adj[from]) - 1, 0, -weight})
}

func (g *DinicGraph) bfs(level map[string]int, start, end string) bool {
	for node := range g.adj {
		level[node] = -1
	}
	queue := []string{start}
	level[start] = 0

	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]

		for _, edge := range g.adj[u] {
			if edge.capacity > 0 && level[edge.target] == -1 {
				level[edge.target] = level[u] + 1
				queue = append(queue, edge.target)
				if edge.target == end {
					return true
				}
			}
		}
	}
	return false
}

func (g *DinicGraph) dfs(level map[string]int, ptr map[string]int, u, end string, flow float64) float64 {
	if u == end {
		return flow
	}

	for ; ptr[u] < len(g.adj[u]); ptr[u]++ {
		edge := g.adj[u][ptr[u]]
		if edge.capacity > 0 && level[u] < level[edge.target] {
			minFlow := flow
			if edge.capacity < flow {
				minFlow = edge.capacity
			}

			pushed := g.dfs(level, ptr, edge.target, end, minFlow)
			if pushed > 0 {
				g.adj[u][ptr[u]].capacity -= pushed
				g.adj[edge.target][edge.rev].capacity += pushed
				return pushed
			}
		}
	}
	return 0
}

func (g *DinicGraph) maxFlow(start, end string) float64 {
	flow := 0.0
	level := make(map[string]int)

	for g.bfs(level, start, end) {
		ptr := make(map[string]int)
		for {
			pushed := g.dfs(level, ptr, start, end, math.Inf(1))
			if pushed == 0 {
				break
			}
			flow += pushed
		}
	}
	return flow
}

func (g *DinicGraph) findPath(start, end string, visited map[string]bool) []string {
	if start == end {
		return []string{end}
	}

	visited[start] = true
	for _, edge := range g.adj[start] {
		if edge.capacity > 0 && !visited[edge.target] {
			path := g.findPath(edge.target, end, visited)
			if len(path) > 0 {
				return append([]string{start}, path...)
			}
		}
	}
	return nil
}

// maxFlowPaths generates multiple candidate paths using a max flow approach
func (os *ONEWANSolver) maxFlowPaths(start, end string, graph_ map[string][]*graph.Edge, maxCandidates int, logger *slog.Logger) ([]routing.PathInfo, error) {
	var candidates []routing.PathInfo

	// Build Dinic graph
	dinicGraph := NewDinicGraph()
	for _, edge := range os.edges {
		// Set capacity to 1 for each edge (single path can use each edge once)
		dinicGraph.addEdge(edge.SourceIp, edge.DestinationIp, 1.0, edge.EdgeWeight)
	}

	// Find multiple paths using max flow
	for len(candidates) < maxCandidates {
		// Find an augmenting path
		visited := make(map[string]bool)
		path := dinicGraph.findPath(start, end, visited)
		if len(path) == 0 {
			// No more paths found
			break
		}

		// Calculate path cost
		cost := os.calculatePathCost(path, graph_)
		newPath := routing.PathInfo{
			Hops: path,
			Rtt:  cost,
		}

		// Add to candidates if not already present
		if !os.pathExists(newPath, candidates) {
			candidates = append(candidates, newPath)
		}

		// Reduce capacity along the path to find different paths
		for i := 0; i < len(path)-1; i++ {
			source := path[i]
			dest := path[i+1]
			for j, edge := range dinicGraph.adj[source] {
				if edge.target == dest {
					dinicGraph.adj[source][j].capacity = 0
					break
				}
			}
		}
	}

	// If we don't have enough paths, fall back to KSP
	if len(candidates) < maxCandidates {
		kspSolver := NewKShortestSolver(os.edges, maxCandidates-len(candidates))
		kspPaths, err := kspSolver.Computing(start, end, "max_flow_fallback", logger)
		if err == nil {
			for _, path := range kspPaths {
				if !os.pathExists(path, candidates) {
					candidates = append(candidates, path)
					if len(candidates) >= maxCandidates {
						break
					}
				}
			}
		}
	}

	return candidates, nil
}

// diversityFilter filters candidate paths to ensure diversity while preserving as many paths as possible
func (os *ONEWANSolver) diversityFilter(candidates []routing.PathInfo) []routing.PathInfo {
	if len(candidates) <= 1 {
		return candidates
	}

	// Calculate similarity between all pairs of paths
	type pathWithSimilarity struct {
		path              routing.PathInfo
		averageSimilarity float64
	}

	var pathsWithSimilarity []pathWithSimilarity
	for i, path := range candidates {
		totalSimilarity := 0.0
		count := 0
		for j, otherPath := range candidates {
			if i != j {
				sim := os.calculatePathSimilarity(path, otherPath)
				totalSimilarity += sim
				count++
			}
		}
		averageSimilarity := 0.0
		if count > 0 {
			averageSimilarity = totalSimilarity / float64(count)
		}
		pathsWithSimilarity = append(pathsWithSimilarity, pathWithSimilarity{
			path:              path,
			averageSimilarity: averageSimilarity,
		})
	}

	// Sort by similarity ascending (more unique paths first)
	sort.Slice(pathsWithSimilarity, func(i, j int) bool {
		return pathsWithSimilarity[i].averageSimilarity < pathsWithSimilarity[j].averageSimilarity
	})

	// Select paths, keeping as many as possible while ensuring diversity
	var selected []routing.PathInfo
	for _, pws := range pathsWithSimilarity {
		// Check if adding this path maintains diversity
		if len(selected) == 0 {
			// Always add the first path
			selected = append(selected, pws.path)
		} else {
			// Calculate average similarity with already selected paths
			totalSimilarity := 0.0
			for _, existingPath := range selected {
				sim := os.calculatePathSimilarity(pws.path, existingPath)
				totalSimilarity += sim
			}
			averageSimilarity := totalSimilarity / float64(len(selected))

			// Add the path if it's not too similar to existing ones
			if averageSimilarity < 0.7 { // Threshold for diversity
				selected = append(selected, pws.path)
			}
		}
	}

	return selected
}

// calculatePathSimilarity calculates the similarity between two paths
func (os *ONEWANSolver) calculatePathSimilarity(path1, path2 routing.PathInfo) float64 {
	nodeSet := make(map[string]bool)
	for _, node := range path1.Hops {
		nodeSet[node] = true
	}

	commonCount := 0
	for _, node := range path2.Hops {
		if nodeSet[node] {
			commonCount++
		}
	}

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

// isValidPath checks if a path is valid in the graph
func (os *ONEWANSolver) isValidPath(hops []string, graph_ map[string][]*graph.Edge) bool {
	if len(hops) < 2 {
		return false
	}

	for i := 0; i < len(hops)-1; i++ {
		source := hops[i]
		dest := hops[i+1]
		found := false
		for _, edge := range graph_[source] {
			if edge.DestinationIp == dest {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// calculatePathCost calculates the cost of a path
func (os *ONEWANSolver) calculatePathCost(hops []string, graph_ map[string][]*graph.Edge) float64 {
	cost := 0.0
	for i := 0; i < len(hops)-1; i++ {
		source := hops[i]
		dest := hops[i+1]
		for _, edge := range graph_[source] {
			if edge.DestinationIp == dest {
				cost += edge.EdgeWeight * os.alpha
				break
			}
		}
	}
	return cost
}

// pathExists checks if a path already exists in the candidate list
func (os *ONEWANSolver) pathExists(path routing.PathInfo, candidates []routing.PathInfo) bool {
	for _, candidate := range candidates {
		if len(candidate.Hops) != len(path.Hops) {
			continue
		}
		match := true
		for i := range candidate.Hops {
			if candidate.Hops[i] != path.Hops[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
