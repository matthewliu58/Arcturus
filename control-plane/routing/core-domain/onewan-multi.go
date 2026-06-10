package core_domain

import (
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
)

// ONEWANSolver implements ONE-WAN path selection algorithm
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
		alpha:    1,
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
	candidates, err := os.maxFlowPaths(start, end, graph_, os.maxPaths, logger)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no paths found from %s to %s", start, end)
	}

	// 2. Apply diversity filter
	filtered := os.diversityFilter(candidates)

	// 3. Select paths up to maxPaths
	var selectedPaths []routing.PathInfo
	for i, path := range filtered {
		if i >= os.maxPaths {
			break
		}
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
	// Initialize all nodes in the graph
	for node := range g.adj {
		level[node] = -1
	}
	// Ensure start and end nodes are initialized
	if _, ok := level[start]; !ok {
		level[start] = -1
	}
	if _, ok := level[end]; !ok {
		level[end] = -1
	}

	queue := []string{start}
	level[start] = 0

	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]

		for _, edge := range g.adj[u] {
			// Safe access: check if target exists in level map
			targetLevel, exists := level[edge.target]
			if edge.capacity > 0 && (!exists || targetLevel == -1) {
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
		// Safe access: check level values exist
		uLevel, uOk := level[u]
		targetLevel, targetOk := level[edge.target]
		if edge.capacity > 0 && uOk && targetOk && uLevel < targetLevel {
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

func (g *DinicGraph) findPath(start, end string, visited map[string]bool, depth, maxDepth int) []string {
	// Depth limit to prevent stack overflow on cyclic graphs
	if depth > maxDepth {
		return nil
	}

	// Check if start node exists in the graph
	if _, ok := g.adj[start]; !ok {
		return nil
	}

	if start == end {
		return []string{start}
	}

	visited[start] = true
	for _, edge := range g.adj[start] {
		if edge.capacity > 0 && !visited[edge.target] {
			path := g.findPath(edge.target, end, visited, depth+1, maxDepth)
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

	// Handle start == end case: single node path
	if start == end {
		cost := os.calculatePathCost([]string{start}, graph_)
		return []routing.PathInfo{{Hops: []string{start}, Rtt: cost}}, nil
	}

	// Build Dinic graph
	dinicGraph := NewDinicGraph()
	for _, edge := range os.edges {
		// Set capacity to 1 for each edge (single path can use each edge once)
		dinicGraph.addEdge(edge.SourceIp, edge.DestinationIp, 1.0, edge.EdgeWeight)
	}

	// Find multiple paths using max flow
	const maxPathDepth = 100 // Max depth to prevent stack overflow on cyclic graphs
	for len(candidates) < maxCandidates {
		// Find an augmenting path
		visited := make(map[string]bool)
		path := dinicGraph.findPath(start, end, visited, 0, maxPathDepth)
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

// ComputingMulti finds diverse paths from one start to multiple destinations.
// For each destination, up to maxPaths paths are found independently.
// Destinations that are unreachable are skipped; an error is returned only if
// no paths can be found for any destination.
func (os *ONEWANSolver) ComputingMulti(start string, ends []string, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	return ComputingMulti(os, start, ends, pre, logger)
}

// ComputingMulti finds diverse paths from one start to multiple destinations.
// Uses global load-aware selection to avoid hotspot congestion.
//
// Algorithm:
// 1. Generate K candidate paths for each destination (already done)
// 2. Select paths jointly considering global node load using Beam Search
// 3. Maintain top K global states during selection
func ComputingMulti(solver *ONEWANSolver, start string, ends []string, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	if len(ends) == 0 {
		return solver.Computing(start, "", pre, logger)
	}

	if len(ends) == 1 {
		return solver.Computing(start, ends[0], pre, logger)
	}

	graph_ := make(map[string][]*graph.Edge)
	nodes := make(map[string]struct{})
	for _, e := range solver.edges {
		graph_[e.SourceIp] = append(graph_[e.SourceIp], e)
		nodes[e.SourceIp] = struct{}{}
		nodes[e.DestinationIp] = struct{}{}
	}

	if _, ok := nodes[start]; !ok {
		return nil, fmt.Errorf("start node %s not found", start)
	}

	// Step 1: Generate candidate paths for each destination using Yen's algorithm with pure Latency
	type destCandidates struct {
		end   string
		paths []routing.PathInfo
		probs []float64 // Pre-calculated selection probabilities
	}
	var allCandidates []destCandidates

	logger.Info("ONEWAN multi: generating candidate paths for each destination",
		slog.String("pre", pre),
		slog.String("start", start),
		slog.Any("destinations", ends))

	for _, end := range ends {
		if _, ok := nodes[end]; !ok {
			logger.Warn("ONEWAN multi: destination not in graph, skipping",
				slog.String("pre", pre), slog.String("end", end))
			continue
		}

		// Use Yen's algorithm with pure Latency for candidate path generation
		kspSolver := NewKShortestSolverWithLatency(solver.edges, solver.maxPaths, true)
		paths, err := kspSolver.yensAlgorithm(start, end, graph_, logger)
		if err != nil {
			logger.Warn("ONEWAN multi: yensAlgorithm failed for destination",
				slog.String("pre", pre), slog.String("end", end), slog.Any("err", err))
			continue
		}

		logger.Debug("yensAlgorithm", slog.String("pre", pre), slog.String("end", end), slog.Any("path", paths))

		// Debug: Print KSP paths after sorting
		var pathDebug []string
		for i, p := range paths {
			pathDebug = append(pathDebug, fmt.Sprintf("[%d] cost=%.2f: %s", i+1, p.cost, strings.Join(p.hops, "->")))
		}
		logger.Debug("KSP paths after sorting", slog.String("end", end), slog.String("paths", strings.Join(pathDebug, "; ")))

		// Convert to PathInfo format (KSP already calculates correct latency when useLatency=true)
		var pathInfos []routing.PathInfo
		for _, path := range paths {
			// Safety: remove consecutive duplicate nodes (e.g., ...->Paris->Paris)
			cleanHops := deduplicateHops(path.hops)

			// Debug: Print path info to verify initialization
			logger.Info("ONEWAN multi: candidate path initialized",
				slog.String("pre", pre),
				slog.String("end", end),
				slog.String("hops", strings.Join(cleanHops, "->")),
				slog.Float64("cost", path.cost),
				slog.Float64("rawRTT", path.rawRTT))

			pathInfos = append(pathInfos, routing.PathInfo{
				Hops:   cleanHops,
				Rtt:    path.cost, // KSP returns correct latency when useLatency=true
				RawRTT: path.rawRTT,
			})
		}

		// Log candidate paths for this destination
		logger.Info("ONEWAN multi: candidate paths for destination",
			slog.String("pre", pre),
			slog.String("end", end),
			slog.Int("candidate_count", len(pathInfos)))
		for i, p := range pathInfos {
			logger.Debug("ONEWAN multi: candidate path",
				slog.String("pre", pre),
				slog.String("end", end),
				slog.Int("index", i),
				slog.Int("hops", len(p.Hops)),
				slog.Float64("latency", p.Rtt),
				slog.Float64("rawRTT", p.RawRTT),
				slog.String("hops", strings.Join(p.Hops, "->")))
		}

		if len(pathInfos) > 0 {
			// Limit to maxPaths candidates per destination
			if len(pathInfos) > solver.maxPaths {
				pathInfos = pathInfos[:solver.maxPaths]
			}
			allCandidates = append(allCandidates, destCandidates{end: end, paths: pathInfos})
		}
	}

	if len(allCandidates) == 0 {
		return nil, fmt.Errorf("no paths found from %s to any of %v", start, ends)
	}

	// Step 2: Stochastic Beam Search for global optimization
	// - Each destination has K paths sorted by latency (shorter = higher selection probability)
	// - For each iteration, randomly select 2 paths per destination based on probability
	// - After N iterations, pick the best solution based on global score

	const numIterations = 10
	const pathsPerDest = 2 // Number of paths to select per destination
	const maxHops = 10     // Maximum allowed hops per path

	// Step 2a: Filter out paths with > maxHops (10) - KSP returns 5 paths, these are candidates
	for i := range allCandidates {
		allCandidates[i].paths = filterPathsByHops(allCandidates[i].paths, maxHops)
		if len(allCandidates[i].paths) == 0 {
			logger.Warn("no valid paths found for destination (all paths exceed maxHops), skipping",
				slog.String("dest", allCandidates[i].end),
				slog.Int("maxHops", maxHops))
			// Skip this destination, continue with others
			continue
		}
	}

	// All paths after filtering are candidates (no limit - use all 5 from KSP)

	// Step 2b: Pre-calculate selection probabilities for each destination's paths
	// Probability = (1/RTT) / sum(1/RTT) - shorter latency = higher probability
	for i := range allCandidates {
		if len(allCandidates[i].paths) == 0 {
			continue
		}
		// Calculate total inverse latency
		totalInverseLatency := 0.0
		for _, p := range allCandidates[i].paths {
			if p.RawRTT > 0 {
				totalInverseLatency += 1.0 / p.RawRTT
			}
		}
		// Calculate probability for each path
		allCandidates[i].probs = make([]float64, len(allCandidates[i].paths))
		for j, p := range allCandidates[i].paths {
			if p.RawRTT > 0 && totalInverseLatency > 0 {
				allCandidates[i].probs[j] = (1.0 / p.RawRTT) / totalInverseLatency
			} else {
				allCandidates[i].probs[j] = 1.0 / float64(len(allCandidates[i].paths))
			}
		}
	}

	// For each destination, calculate probabilities for its paths
	type scoredSolution struct {
		paths []routing.PathInfo
		score float64
	}

	var allSolutions []scoredSolution

	// Run N iterations of random path selection
	for iter := 0; iter < numIterations; iter++ {
		var selectedPaths []routing.PathInfo
		globalLoad := make(map[string]float64)

		// For each destination, select pathsPerDest DIFFERENT paths based on pre-calculated probabilities
		for _, dc := range allCandidates {
			if len(dc.paths) == 0 {
				continue
			}
			// Track which paths have already been selected
			selectedIndices := make(map[int]bool)

			// Select pathsPerDest unique paths
			for selectIdx := 0; selectIdx < pathsPerDest && selectIdx < len(dc.paths); selectIdx++ {
				// Calculate cumulative probability excluding already selected paths
				totalProb := 0.0
				for pathIdx, prob := range dc.probs {
					if !selectedIndices[pathIdx] {
						totalProb += prob
					}
				}

				// Generate random value [0, totalProb)
				r := rand.Float64() * totalProb

				// Select a path based on probability
				cumulative := 0.0
				selectedPathIdx := 0
				for pathIdx, prob := range dc.probs {
					if selectedIndices[pathIdx] {
						continue
					}
					cumulative += prob
					if r <= cumulative {
						selectedPathIdx = pathIdx
						break
					}
				}

				selectedIndices[selectedPathIdx] = true
				selectedPath := dc.paths[selectedPathIdx]
				selectedPaths = append(selectedPaths, selectedPath)

				// Update global load (each node counted once per path)
				processedNodes := make(map[string]bool)
				for _, node := range selectedPath.Hops {
					if !processedNodes[node] {
						processedNodes[node] = true
						globalLoad[node] += 1.0
					}
				}
			}
		}

		// Calculate global score for this solution
		// Score = sum of path latencies + load penalty
		totalScore := 0.0
		const loadWeight = 10.0

		// Recalculate score based on selected paths
		processedNodes := make(map[string]bool)
		for _, path := range selectedPaths {
			totalScore += path.RawRTT // Base latency

			for _, node := range path.Hops {
				if !processedNodes[node] {
					processedNodes[node] = true
					load := globalLoad[node]
					totalScore += load * load * loadWeight // Load penalty
				}
			}
		}

		allSolutions = append(allSolutions, scoredSolution{
			paths: selectedPaths,
			score: totalScore,
		})

		// Build path strings for logging
		var pathStrs []string
		for _, p := range selectedPaths {
			pathStrs = append(pathStrs, fmt.Sprintf("%s(%.0fms)", strings.Join(p.Hops, "->"), p.RawRTT))
		}

		logger.Debug("ONEWAN multi: generated solution",
			slog.String("pre", pre),
			slog.Int("iteration", iter),
			slog.Int("path_count", len(selectedPaths)),
			slog.Any("paths", pathStrs),
			slog.Float64("score", totalScore))
	}

	// Find best solution (lowest score = best)
	bestSolution := allSolutions[0]
	for _, sol := range allSolutions[1:] {
		if sol.score < bestSolution.score {
			bestSolution = sol
		}
	}

	// Build best path strings for logging
	var bestPathStrs []string
	for _, p := range bestSolution.paths {
		bestPathStrs = append(bestPathStrs, fmt.Sprintf("%s(%.0fms)", strings.Join(p.Hops, "->"), p.RawRTT))
	}

	logger.Info("ONEWAN multi paths selected (stochastic beam search)",
		slog.String("pre", pre),
		slog.String("start", start),
		slog.Int("dest_count", len(allCandidates)),
		slog.Int("total_paths", len(bestSolution.paths)),
		slog.Any("selected_paths", bestPathStrs),
		slog.Float64("global_score", bestSolution.score),
		slog.Int("solutions_evaluated", len(allSolutions)))

	return bestSolution.paths, nil
}

// filterPathsByHops filters out paths with more than maxHops hops
func filterPathsByHops(paths []routing.PathInfo, maxHops int) []routing.PathInfo {
	var result []routing.PathInfo
	for _, path := range paths {
		if len(path.Hops)-1 <= maxHops {
			result = append(result, path)
		}
	}
	return result
}

// deduplicateHops removes consecutive duplicate nodes from a path.
// e.g., [A, B, B, C] → [A, B, C]
func deduplicateHops(hops []string) []string {
	if len(hops) <= 1 {
		return hops
	}
	result := []string{hops[0]}
	for i := 1; i < len(hops); i++ {
		if hops[i] != hops[i-1] {
			result = append(result, hops[i])
		}
	}
	return result
}
