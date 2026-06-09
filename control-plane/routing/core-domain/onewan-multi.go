package core_domain

import (
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
)

// ComputingMulti finds diverse paths from one start to multiple destinations.
// For each destination, up to maxPaths paths are found independently.
// Destinations that are unreachable are skipped; an error is returned only if
// no paths can be found for any destination.
//
// This is a standalone variant that mirrors ONEWANSolver.ComputingMulti
// but does not mutate the solver state.
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

	type destCandidates struct {
		end   string
		paths []routing.PathInfo
	}
	var allCandidates []destCandidates

	for _, end := range ends {
		if _, ok := nodes[end]; !ok {
			logger.Warn("ONEWAN multi: destination not in graph, skipping",
				slog.String("pre", pre), slog.String("end", end))
			continue
		}

		candidates, err := solver.maxFlowPaths(start, end, graph_, solver.maxPaths, logger)
		if err != nil {
			logger.Warn("ONEWAN multi: maxFlowPaths failed for destination",
				slog.String("pre", pre), slog.String("end", end), slog.Any("err", err))
			continue
		}

		filtered := solver.diversityFilter(candidates)
		if len(filtered) > solver.maxPaths {
			filtered = filtered[:solver.maxPaths]
		}

		if len(filtered) > 0 {
			allCandidates = append(allCandidates, destCandidates{end: end, paths: filtered})
		}
	}

	if len(allCandidates) == 0 {
		return nil, fmt.Errorf("no paths found from %s to any of %v", start, ends)
	}

	// Interleave paths in round-robin order across destinations.
	var result []routing.PathInfo
	indices := make([]int, len(allCandidates))
	total := solver.maxPaths * len(allCandidates)
	for len(result) < total {
		added := false
		for i, dc := range allCandidates {
			if indices[i] < len(dc.paths) {
				result = append(result, dc.paths[indices[i]])
				indices[i]++
				added = true
			}
		}
		if !added {
			break
		}
	}

	logger.Info("ONEWAN multi paths selected", slog.String("pre", pre),
		slog.String("start", start), slog.Int("dest_count", len(allCandidates)),
		slog.Int("total_paths", len(result)))

	return result, nil
}
