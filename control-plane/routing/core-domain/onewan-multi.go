package core_domain

import (
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
)

const maxHops = 10 // maximum hop limit

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

// ComputingMulti finds diverse paths from one start to multiple destinations.
// For each destination, up to maxPaths paths are found independently.
// Destinations that are unreachable are skipped; an error is returned only if
// no paths can be found for any destination.
// destCandidates holds candidate paths and their selection probabilities for a destination
type destCandidates struct {
	end   string
	paths []routing.PathInfo
	probs []float64
}

func (os *ONEWANSolver) ComputingMulti(start string, ends []string, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {

	// Build graph structure
	graph_ := make(map[string][]*graph.Edge)
	nodes := make(map[string]struct{})
	for _, e := range os.edges {
		graph_[e.SourceIp] = append(graph_[e.SourceIp], e)
		nodes[e.SourceIp] = struct{}{}
		nodes[e.DestinationIp] = struct{}{}
	}

	if _, ok := nodes[start]; !ok {
		return nil, fmt.Errorf("start node %s not found", start)
	}

	// Step 1: Generate candidate paths for each destination (with maxHops filter)
	allCandidates, err := os.generateCandidatePaths(start, ends, nodes, graph_, pre, logger)
	if err != nil {
		return nil, err
	}

	// Step 2: Select best paths using Beam Search
	return os.beamSearchSelection(allCandidates, pre, logger)
}

// generateCandidatePaths generates candidate paths for each destination using DFS
func (os *ONEWANSolver) generateCandidatePaths(start string, ends []string, nodes map[string]struct{},
	graph_ map[string][]*graph.Edge, pre string, logger *slog.Logger) ([]destCandidates, error) {

	var allCandidates []destCandidates

	logger.Info("ONEWAN multi: generating candidate paths for each destination",
		slog.String("pre", pre), slog.String("start", start), slog.Any("destinations", ends))

	for _, end := range ends {
		if _, ok := nodes[end]; !ok {
			logger.Warn("ONEWAN multi: destination not in graph, skipping",
				slog.String("pre", pre), slog.String("end", end))
			continue
		}

		// Use DFS to randomly generate multiple paths
		paths := randomDFSPaths(start, end, graph_, os.maxPaths, logger)
		if len(paths) == 0 {
			logger.Warn("ONEWAN multi: randomDFSPaths failed for destination",
				slog.String("pre", pre), slog.String("end", end))
			continue
		}

		//logger.Debug("randomDFSPaths", slog.String("pre", pre), slog.String("end", end), slog.Int("path_count", len(paths)))

		var pathDebug []string
		for i, p := range paths {
			pathDebug = append(pathDebug, fmt.Sprintf("[%d] cost=%.2f: %s", i+1, p.cost, strings.Join(p.hops, "->")))
		}
		//logger.Debug("DFS random paths", slog.String("end", end), slog.String("paths", strings.Join(pathDebug, "; ")))

		var pathInfos []routing.PathInfo
		for _, path := range paths {
			cleanHops := deduplicateHops(path.hops)

			// Filter by maxHops
			if len(cleanHops)-1 > maxHops {
				continue
			}

			pathInfos = append(pathInfos, routing.PathInfo{
				Hops:   cleanHops,
				Rtt:    path.cost,
				RawRTT: path.rawRTT,
			})
		}

		pathInfos = filterPathsByHops(pathInfos, maxHops)
		if len(pathInfos) == 0 {
			logger.Warn("ONEWAN multi: no valid paths found for destination (all paths exceed maxHops)",
				slog.String("pre", pre),
				slog.String("end", end),
				slog.Int("maxHops", maxHops))
			continue
		}

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
			if len(pathInfos) > os.maxPaths {
				pathInfos = pathInfos[:os.maxPaths]
			}
			allCandidates = append(allCandidates, destCandidates{end: end, paths: pathInfos})
		}
	}

	if len(allCandidates) == 0 {
		return nil, fmt.Errorf("no paths found from %s to any of %v", start, ends)
	}

	return allCandidates, nil
}

// beamSearchSelection selects best paths using stochastic beam search
func (os *ONEWANSolver) beamSearchSelection(allCandidates []destCandidates, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	const numIterations = 10
	const pathsPerDest = 2
	const loadWeight = 5.0

	// Calculate selection probabilities based on inverse latency
	for i := range allCandidates {
		if len(allCandidates[i].paths) == 0 {
			continue
		}
		totalInverseLatency := 0.0
		for _, p := range allCandidates[i].paths {
			if p.RawRTT > 0 {
				totalInverseLatency += 1.0 / p.RawRTT
			}
		}
		allCandidates[i].probs = make([]float64, len(allCandidates[i].paths))
		for j, p := range allCandidates[i].paths {
			if p.RawRTT > 0 && totalInverseLatency > 0 {
				allCandidates[i].probs[j] = (1.0 / p.RawRTT) / totalInverseLatency
			} else {
				allCandidates[i].probs[j] = 1.0 / float64(len(allCandidates[i].paths))
			}
		}
	}

	// Build edge load map
	edgeLoadMap := make(map[string]float64)
	for _, e := range os.edges {
		edgeLoadMap[e.SourceIp+"->"+e.DestinationIp] = e.Load
	}

	type scoredSolution struct {
		paths []routing.PathInfo
		score float64
	}

	var allSolutions []scoredSolution

	// Stochastic beam search iterations
	for iter := 0; iter < numIterations; iter++ {
		var selectedPaths []routing.PathInfo
		edgeUsage := make(map[string]float64)

		for _, dc := range allCandidates {
			if len(dc.paths) == 0 {
				continue
			}
			selectedIndices := make(map[int]bool)

			for selectIdx := 0; selectIdx < pathsPerDest && selectIdx < len(dc.paths); selectIdx++ {
				totalProb := 0.0
				for pathIdx, prob := range dc.probs {
					if !selectedIndices[pathIdx] {
						totalProb += prob
					}
				}

				r := rand.Float64() * totalProb

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

				hops := selectedPath.Hops
				for i := 0; i < len(hops)-1; i++ {
					edgeKey := hops[i] + "->" + hops[i+1]
					edgeUsage[edgeKey] += 1.0
				}
			}
		}

		totalScore := 0.0
		processedEdges := make(map[string]bool)
		for _, path := range selectedPaths {
			totalScore += path.RawRTT

			hops := path.Hops
			for i := 0; i < len(hops)-1; i++ {
				edgeKey := hops[i] + "->" + hops[i+1]
				if !processedEdges[edgeKey] {
					processedEdges[edgeKey] = true
					count := edgeUsage[edgeKey]
					realLoad := edgeLoadMap[edgeKey]
					penalty := count * count * max(realLoad, 1.0) * loadWeight
					totalScore += penalty
				}
			}
		}

		allSolutions = append(allSolutions, scoredSolution{
			paths: selectedPaths,
			score: totalScore,
		})

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

	// Find best solution
	bestSolution := allSolutions[0]
	for _, sol := range allSolutions[1:] {
		if sol.score < bestSolution.score {
			bestSolution = sol
		}
	}

	var bestPathStrs []string
	for _, p := range bestSolution.paths {
		bestPathStrs = append(bestPathStrs, fmt.Sprintf("%s(%.0fms)", strings.Join(p.Hops, "->"), p.RawRTT))
	}

	logger.Info("ONEWAN multi paths selected (stochastic beam search)",
		slog.String("pre", pre),
		slog.Int("dest_count", len(allCandidates)),
		slog.Int("total_paths", len(bestSolution.paths)),
		slog.Any("selected_paths", bestPathStrs),
		slog.Float64("global_score", bestSolution.score),
		slog.Int("solutions_evaluated", len(allSolutions)))

	return bestSolution.paths, nil
}

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

// randomDFSPaths uses DFS with randomization to generate multiple diverse paths
func randomDFSPaths(start, end string, graph_ map[string][]*graph.Edge, maxPaths int, logger *slog.Logger) []Path {
	const maxAttempts = 100
	const maxHops = 15

	var paths []Path
	usedPaths := make(map[string]bool) // Track unique paths by their string representation

	for attempt := 0; attempt < maxAttempts && len(paths) < maxPaths; attempt++ {
		path := dfsRandomPath(start, end, graph_, maxHops, logger)
		if path != nil {
			pathStr := strings.Join(path.hops, "->")
			if !usedPaths[pathStr] {
				usedPaths[pathStr] = true
				paths = append(paths, *path)
				logger.Debug("randomDFSPaths: found unique path",
					slog.Int("attempt", attempt),
					slog.Int("total_paths", len(paths)),
					slog.Int("hop", len(path.hops)),
					slog.Any("rtt", path.rawRTT),
					slog.String("path", pathStr))
			}
		}
	}

	// Sort paths by cost (latency)
	for i := 0; i < len(paths)-1; i++ {
		for j := i + 1; j < len(paths); j++ {
			if paths[j].cost < paths[i].cost {
				paths[i], paths[j] = paths[j], paths[i]
			}
		}
	}

	return paths
}

// dfsRandomPath performs a single DFS with randomization to find a path
func dfsRandomPath(current, target string, graph_ map[string][]*graph.Edge, maxHops int, logger *slog.Logger) *Path {
	visited := make(map[string]bool)
	var hops []string
	var totalLatency float64

	var dfs func(node string, depth int) bool
	dfs = func(node string, depth int) bool {
		if depth > maxHops {
			return false
		}

		visited[node] = true
		hops = append(hops, node)

		if node == target {
			return true
		}

		// Get neighbors and shuffle for randomness
		neighbors := graph_[node]
		if len(neighbors) == 0 {
			hops = hops[:len(hops)-1]
			delete(visited, node)
			return false
		}

		// Shuffle neighbors for randomization
		indices := rand.Perm(len(neighbors))
		for _, idx := range indices {
			edge := neighbors[idx]
			next := edge.DestinationIp
			if !visited[next] {
				if dfs(next, depth+1) {
					totalLatency += edge.Latency
					return true
				}
			}
		}

		// Backtrack
		hops = hops[:len(hops)-1]
		delete(visited, node)
		return false
	}

	if dfs(current, 0) {
		return &Path{
			hops:   hops,
			cost:   totalLatency,
			rawRTT: totalLatency,
		}
	}
	return nil
}

func filterPathsByHops(paths []routing.PathInfo, maxHops int) []routing.PathInfo {
	var result []routing.PathInfo
	for _, path := range paths {
		if len(path.Hops)-1 <= maxHops {
			result = append(result, path)
		}
	}
	return result
}
