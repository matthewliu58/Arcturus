package core_domain

import (
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
	"math/rand/v2"
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
		return nil, fmt.Errorf("no destinations provided")
	}

	if len(ends) == 1 {
		kspSolver := NewKShortestSolverWithLatency(solver.edges, solver.maxPaths, true)
		graph_ := make(map[string][]*graph.Edge)
		for _, e := range solver.edges {
			graph_[e.SourceIp] = append(graph_[e.SourceIp], e)
		}
		paths, err := kspSolver.yensAlgorithm(start, ends[0], graph_, logger)
		if err != nil {
			return nil, err
		}
		var pathInfos []routing.PathInfo
		for _, path := range paths {
			cleanHops := deduplicateHops(path.hops)
			pathInfos = append(pathInfos, routing.PathInfo{
				Hops:   cleanHops,
				Rtt:    path.cost,
				RawRTT: path.rawRTT,
			})
		}
		return pathInfos, nil
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

	type destCandidates struct {
		end   string
		paths []routing.PathInfo
		probs []float64
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

		kspSolver := NewKShortestSolverWithLatency(solver.edges, solver.maxPaths, true)
		paths, err := kspSolver.yensAlgorithm(start, end, graph_, logger)
		if err != nil {
			logger.Warn("ONEWAN multi: yensAlgorithm failed for destination",
				slog.String("pre", pre), slog.String("end", end), slog.Any("err", err))
			continue
		}

		logger.Debug("yensAlgorithm", slog.String("pre", pre), slog.String("end", end), slog.Any("path", paths))

		var pathDebug []string
		for i, p := range paths {
			pathDebug = append(pathDebug, fmt.Sprintf("[%d] cost=%.2f: %s", i+1, p.cost, strings.Join(p.hops, "->")))
		}
		logger.Debug("KSP paths after sorting", slog.String("end", end), slog.String("paths", strings.Join(pathDebug, "; ")))

		var pathInfos []routing.PathInfo
		for _, path := range paths {
			cleanHops := deduplicateHops(path.hops)

			logger.Info("ONEWAN multi: candidate path initialized",
				slog.String("pre", pre),
				slog.String("end", end),
				slog.String("hops", strings.Join(cleanHops, "->")),
				slog.Float64("cost", path.cost),
				slog.Float64("rawRTT", path.rawRTT))

			pathInfos = append(pathInfos, routing.PathInfo{
				Hops:   cleanHops,
				Rtt:    path.cost,
				RawRTT: path.rawRTT,
			})
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
			if len(pathInfos) > solver.maxPaths {
				pathInfos = pathInfos[:solver.maxPaths]
			}
			allCandidates = append(allCandidates, destCandidates{end: end, paths: pathInfos})
		}
	}

	if len(allCandidates) == 0 {
		return nil, fmt.Errorf("no paths found from %s to any of %v", start, ends)
	}

	const numIterations = 10
	const pathsPerDest = 2
	const maxHops = 10

	for i := range allCandidates {
		allCandidates[i].paths = filterPathsByHops(allCandidates[i].paths, maxHops)
		if len(allCandidates[i].paths) == 0 {
			logger.Warn("no valid paths found for destination (all paths exceed maxHops), skipping",
				slog.String("dest", allCandidates[i].end),
				slog.Int("maxHops", maxHops))
			continue
		}
	}

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

	type scoredSolution struct {
		paths []routing.PathInfo
		score float64
	}

	edgeLoadMap := make(map[string]float64)
	for _, e := range solver.edges {
		edgeLoadMap[e.SourceIp+"->"+e.DestinationIp] = e.Load
	}

	var allSolutions []scoredSolution

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
		const loadWeight = 5.0

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
		slog.String("start", start),
		slog.Int("dest_count", len(allCandidates)),
		slog.Int("total_paths", len(bestSolution.paths)),
		slog.Any("selected_paths", bestPathStrs),
		slog.Float64("global_score", bestSolution.score),
		slog.Int("solutions_evaluated", len(allSolutions)))

	return bestSolution.paths, nil
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
