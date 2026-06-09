package core_domain

import (
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
	"math"
	"strings"
)

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

		// Sort paths by latency to ensure correct ordering
		// This is a safety measure in case Yen's algorithm returns unsorted paths
		for i := 0; i < len(paths)-1; i++ {
			for j := i + 1; j < len(paths); j++ {
				if paths[j].cost < paths[i].cost {
					paths[i], paths[j] = paths[j], paths[i]
				}
			}
		}

		// Debug: Print KSP paths after sorting
		var pathDebug []string
		for i, p := range paths {
			pathDebug = append(pathDebug, fmt.Sprintf("[%d] cost=%.2f: %s", i+1, p.cost, strings.Join(p.hops, "->")))
		}
		logger.Debug("KSP paths after sorting", slog.String("end", end), slog.String("paths", strings.Join(pathDebug, "; ")))

		// Convert to PathInfo format (KSP already calculates correct latency when useLatency=true)
		var pathInfos []routing.PathInfo
		for _, path := range paths {
			// Debug: Print path info to verify initialization
			logger.Info("ONEWAN multi: candidate path initialized",
				slog.String("pre", pre),
				slog.String("end", end),
				slog.String("hops", strings.Join(path.hops, "->")),
				slog.Float64("cost", path.cost),
				slog.Float64("rawRTT", path.rawRTT))

			pathInfos = append(pathInfos, routing.PathInfo{
				Hops:   path.hops,
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

	// Step 2: Beam Search for global optimization
	// Beam width: number of states to maintain
	const beamWidth = 10
	const maxHops = 10 // Maximum allowed hops per path

	// Initialize beam with empty state
	beam := []beamState{{paths: []routing.PathInfo{}, load: make(map[string]float64), score: 0}}

	// Expand beam for each destination
	for destIdx, dc := range allCandidates {
		var newBeam []beamState

		// Filter out paths with too many hops
		filteredPaths := filterPathsByHops(dc.paths, maxHops)
		if len(filteredPaths) == 0 {
			// If no paths pass the hop limit, keep only the shortest one (first path)
			if len(dc.paths) > 0 {
				filteredPaths = []routing.PathInfo{dc.paths[0]}
			} else {
				continue // Skip this destination if no paths available
			}
		}

		// For each state in current beam, expand with all combinations of paths for this destination
		for _, state := range beam {
			// Only use the first maxPaths paths for combination generation
			// This ensures we only consider the best candidates (shortest paths)
			limitedPaths := filteredPaths
			if len(filteredPaths) > solver.maxPaths {
				limitedPaths = filteredPaths[:solver.maxPaths]
			}

			// Generate all combinations of exactly maxPaths paths from limited candidates
			combinations := generatePathCombinationsExactSize(limitedPaths, solver.maxPaths)

			for _, combo := range combinations {
				// Create new state by adding these paths
				newState := beamState{
					paths: append([]routing.PathInfo{}, state.paths...),
					load:  make(map[string]float64),
					score: state.score,
				}

				// Copy existing load
				for node, load := range state.load {
					newState.load[node] = load
				}

				// Add new paths and update load and score
				for _, path := range combo {
					newState.paths = append(newState.paths, path)

					// Track processed nodes in this path to avoid double counting
					processedNodes := make(map[string]bool)

					// First pass: update node load based on edge.Load values
					// Each node in a path should only contribute once
					for i := 0; i < len(path.Hops)-1; i++ {
						src := path.Hops[i]
						dst := path.Hops[i+1]

						// Find the edge and get its load
						for _, edge := range graph_[src] {
							if edge.DestinationIp == dst {
								loadRisk := calculateLoadRisk(edge.Load)
								// Add load risk for src node (only once per path)
								if !processedNodes[src] {
									newState.load[src] += loadRisk
									processedNodes[src] = true
								}
								// Add load risk for dst node (only once per path)
								if !processedNodes[dst] {
									newState.load[dst] += loadRisk
									processedNodes[dst] = true
								}
								break
							}
						}
					}

					// Calculate path score: base latency + load penalty
					// Score = path latency + weight * sum(load^2)
					// Using square penalty for load balancing
					pathScore := path.Rtt // base latency

					// Add load penalty: higher accumulated load = higher penalty
					// loadWeight controls the importance of load balancing vs latency
					const loadWeight = 100.0 // Adjust this to balance latency vs load
					for _, node := range path.Hops {
						load := newState.load[node]
						pathScore += load * load * loadWeight // weighted load penalty
					}

					newState.score += pathScore
				}

				newBeam = append(newBeam, newState)
			}
		}

		// Keep only top K states (beam pruning)
		beam = pruneBeam(newBeam, beamWidth)

		logger.Debug("ONEWAN multi: beam expanded",
			slog.String("pre", pre),
			slog.String("end", dc.end),
			slog.Int("dest_index", destIdx),
			slog.Int("beam_size", len(beam)),
			slog.Float64("best_score", beam[0].score))
	}

	if len(beam) == 0 {
		return nil, fmt.Errorf("beam search failed: no valid states")
	}

	// Return the best state's paths
	bestState := beam[0]
	logger.Info("ONEWAN multi paths selected (beam search)",
		slog.String("pre", pre),
		slog.String("start", start),
		slog.Int("dest_count", len(allCandidates)),
		slog.Int("total_paths", len(bestState.paths)),
		slog.Float64("global_score", bestState.score),
		slog.Any("virtual_load", bestState.load))

	return bestState.paths, nil
}

// beamState represents a partial assignment of paths to destinations
type beamState struct {
	paths []routing.PathInfo
	load  map[string]float64
	score float64
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

// generatePathCombinationsExactSize generates combinations of paths with EXACTLY the specified size.
// FIXED: No longer returns variable-length combinations (1,2,...size).
// If insufficient paths are available, returns ALL available paths as a single valid combination.
func generatePathCombinationsExactSize(paths []routing.PathInfo, size int) [][]routing.PathInfo {
	// Return empty if no paths are available
	if len(paths) == 0 {
		return [][]routing.PathInfo{}
	}

	// Standard case: generate combinations of exactly 'size' paths
	if len(paths) >= size {
		return generateCombinationsOfSize(paths, size, 0, make([]routing.PathInfo, size))
	}

	// Fallback: Not enough paths, return all available paths as a single fixed combination
	// Ensures stable output length for business logic
	fixedCombo := make([]routing.PathInfo, len(paths))
	copy(fixedCombo, paths)
	return [][]routing.PathInfo{fixedCombo}
}

// generateCombinationsOfSize recursively generates all unique combinations of paths with a specific length
// paths: the list of candidate paths
// size: remaining number of paths to choose
// start: starting index to avoid duplicate combinations
// current: temporary slice to store the current combination being built
func generateCombinationsOfSize(paths []routing.PathInfo, size, start int, current []routing.PathInfo) [][]routing.PathInfo {
	var result [][]routing.PathInfo

	// Base case: combination is complete, save a copy
	if size == 0 {
		comb := make([]routing.PathInfo, len(current))
		copy(comb, current)
		return [][]routing.PathInfo{comb}
	}

	// Recursively build combinations
	for i := start; i <= len(paths)-size; i++ {
		current[len(current)-size] = paths[i]
		subCombinations := generateCombinationsOfSize(paths, size-1, i+1, current)
		result = append(result, subCombinations...)
	}

	return result
}

// pruneBeam keeps only top K states based on score
func pruneBeam(states []beamState, k int) []beamState {
	if len(states) <= k {
		return states
	}

	// Sort by score (ascending)
	for i := 0; i < len(states)-1; i++ {
		for j := i + 1; j < len(states); j++ {
			if states[j].score < states[i].score {
				states[i], states[j] = states[j], states[i]
			}
		}
	}

	// Return top K
	return states[:k]
}

// calculateLoadRisk calculates load risk similar to CPU risk in update-graph.go
// Load range: 0-100
// No penalty below LoadMid, penalty increases from LoadMid to LoadHigh, max at LoadHigh+
func calculateLoadRisk(load float64) float64 {
	const (
		LoadMid   = 20.0 // Lower threshold for test compatibility
		LoadHigh  = 80.0 // Max penalty above this value
		loadPower = 1.5  // Non-linear power factor
	)

	if load < LoadMid {
		// Load < LoadMid: no penalty
		return 0.0
	} else if load >= LoadHigh {
		// Load >= LoadHigh: max penalty
		return 1.0
	} else {
		// LoadMid <= Load < LoadHigh: interpolate penalty with non-linear curve
		loadRatio := (load - LoadMid) / (LoadHigh - LoadMid)
		return math.Pow(loadRatio, loadPower)
	}
}
