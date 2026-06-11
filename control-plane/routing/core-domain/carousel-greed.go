package core_domain

import (
	"container/list"
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"strings"
	"time"
)

type FlowOptimizationSolver struct {
	edges        []*graph.Edge
	alpha        float64            // iteration count coefficient
	beta         float64            // path removal ratio
	capacity     float64            // edge capacity u_ij (dynamically computed: numDestinations * flowsPerDest)
	flowsPerDest int                // number of flows per destination
	Tu           float64            // safe utilization threshold (node is uncongested below this value)
	Uhot         float64            // high-percentile utilization threshold (hotspot condition)
	epsilon      float64            // small constant to prevent division by zero
	thetaLMap    map[string]float64 // destination -> latency constraint
	kspK         int                // number of KSP paths used to compute average latency
	latencyMap   map[string]float64 // edge (source->dest) -> latency (for O(1) lookup)
	rng          *rand.Rand         // random number generator (initialized once)
}

// NewFlowOptimizationSolver creates a new instance
func NewFlowOptimizationSolver(edges []*graph.Edge) *FlowOptimizationSolver {
	// Build latency map for O(1) edge latency lookup
	latencyMap := make(map[string]float64)
	for _, e := range edges {
		key := e.SourceIp + "->" + e.DestinationIp
		latencyMap[key] = e.Latency
	}

	// Initialize random number generator once (reused across iterations)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	return &FlowOptimizationSolver{
		edges:        edges,
		alpha:        2.0,   // iteration count coefficient (increase for more thorough optimization)
		beta:         0.5,   // path removal ratio (aggressive exploration: remove 90% of initial paths)
		capacity:     0.0,   // edge capacity computed dynamically in ComputingMulti based on destination count
		flowsPerDest: 3,     // 3 flows per destination
		Tu:           60.0,  // safe utilization threshold (node is uncongested below this value)
		Uhot:         0.0,   // high-percentile utilization threshold (computed as 90th percentile at runtime)
		epsilon:      0.001, // small constant to prevent division by zero
		thetaLMap:    make(map[string]float64),
		kspK:         3, // use top 5 KSP paths to compute average latency
		latencyMap:   latencyMap,
		rng:          rng,
	}
}

// EdgeFlow records flow information for an edge
type EdgeFlow struct {
	flow     float64 // current flow
	cap      float64 // original capacity u_ij
	thetaA   float64 // dynamic utilization factor [0,1]
	effCap   float64 // effective capacity = thetaA * cap
	banned   bool    // temporarily banned for exploration (not permanently removed)
	residual float64 // residual capacity = effCap - flow (forward residual)
}

// updateResidual updates the residual capacity
func (ef *EdgeFlow) updateResidual() {
	ef.residual = ef.effCap - ef.flow
}

// getReverseResidual returns the reverse residual capacity (equals current flow)
func (ef *EdgeFlow) getReverseResidual() float64 {
	return ef.flow // Backward residual = flow (standard residual network)
}

// computeThetaA computes the dynamic scaling factor theta_a(v) based on CPU utilization
// Formula: U'_v = min(1, max(U_v-Tu,0) / max(U_hot-Tu, epsilon))
//
//	theta_a(v) = 1 - U'_v
func (d *FlowOptimizationSolver) computeThetaA(cpuLoad float64) float64 {
	numerator := cpuLoad - d.Tu
	if numerator < 0 {
		numerator = 0
	}
	denominator := d.Uhot - d.Tu
	if denominator < d.epsilon {
		denominator = d.epsilon
	}
	U_prime := numerator / denominator
	if U_prime > 1.0 {
		U_prime = 1.0
	}
	return 1.0 - U_prime
}

// computeThetaLForDestinations computes average latency for each destination using KSP as theta_L(d)
func (d *FlowOptimizationSolver) computeThetaLForDestinations(start string, ends []string, logger *slog.Logger) {
	// Create KSP solver (use latency as weight)
	kspSolver := NewKShortestSolverWithLatency(d.edges, d.kspK, true)

	for _, end := range ends {
		// Compute top kspK paths using KSP
		paths, err := kspSolver.Computing(start, end, "thetaL_calc", logger)
		if err != nil || len(paths) == 0 {
			// Use default value if no paths found
			d.thetaLMap[end] = 100.0
			logger.Warn("FlowOptimizationSolver: cannot find paths for destination, using default thetaL",
				slog.String("dest", end),
				slog.Float64("thetaL", d.thetaLMap[end]))
			continue
		}

		// Compute average latency using Rtt (computed latency) for consistency with DFS calculation
		totalLatency := 0.0
		for _, path := range paths {
			totalLatency += path.Rtt // Use Rtt instead of RawRTT
		}
		avgLatency := totalLatency / float64(len(paths))

		// Set latency constraint to average latency * 2 to allow longer paths
		d.thetaLMap[end] = avgLatency * 2.0

		logger.Debug("FlowOptimizationSolver: computed thetaL for destination",
			slog.String("dest", end),
			slog.Float64("avgLatency", avgLatency),
			slog.Float64("thetaL", d.thetaLMap[end]))
	}
}

// computePercentile computes the percentile of edge loads
func (d *FlowOptimizationSolver) computePercentile(percentile float64) float64 {
	if len(d.edges) == 0 {
		return 0.0
	}

	// Extract all edge loads
	loads := make([]float64, 0, len(d.edges))
	for _, e := range d.edges {
		loads = append(loads, e.Load)
	}

	// Sort
	for i := 0; i < len(loads)-1; i++ {
		for j := i + 1; j < len(loads); j++ {
			if loads[j] < loads[i] {
				loads[i], loads[j] = loads[j], loads[i]
			}
		}
	}

	// Compute percentile position
	index := percentile / 100.0 * float64(len(loads)-1)
	lowerIndex := int(index)
	upperIndex := lowerIndex + 1

	if upperIndex >= len(loads) {
		return loads[len(loads)-1]
	}

	// Linear interpolation
	fraction := index - float64(lowerIndex)
	return loads[lowerIndex] + fraction*(loads[upperIndex]-loads[lowerIndex])
}

// PathWithInfo contains path information (for multi-destination)
type PathWithInfo struct {
	path    []string
	dest    string
	latency float64
}

// ComputingMulti performs multi-destination flow optimization
// start: source node
// ends: list of destinations (e.g., 10 destinations)
// Returns: list of paths for each destination (max flowsPerDest paths per destination)
func (d *FlowOptimizationSolver) ComputingMulti(start string, ends []string, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	logger.Info("ComputingMulti START",
		slog.String("pre", pre),
		slog.String("start", start),
		slog.Any("ends", ends),
		slog.Int("totalEdges", len(d.edges)))

	if len(ends) == 0 {
		return nil, fmt.Errorf("no destinations provided")
	}

	// Validate source node exists in graph
	nodes := make(map[string]struct{})
	for _, e := range d.edges {
		nodes[e.SourceIp] = struct{}{}
		nodes[e.DestinationIp] = struct{}{}
	}

	logger.Debug("Nodes in graph", slog.String("pre", pre), slog.Int("count", len(nodes)))

	if _, ok := nodes[start]; !ok {
		return nil, fmt.Errorf("start node %s not found in graph", start)
	}

	// Validate all destinations exist in graph
	validEnds := make([]string, 0, len(ends))
	for _, end := range ends {
		if _, ok := nodes[end]; ok {
			validEnds = append(validEnds, end)
		} else {
			logger.Warn("FlowOptimization multi: destination not in graph, skipping",
				slog.String("pre", pre), slog.String("end", end))
		}
	}

	if len(validEnds) == 0 {
		return nil, fmt.Errorf("no valid destinations found in graph from %s", start)
	}

	logger.Debug("Valid destinations", slog.String("pre", pre), slog.Any("destinations", validEnds))

	// Dynamically compute edge capacity: capacity = numDestinations * flowsPerDest
	d.capacity = float64(len(validEnds) * d.flowsPerDest)

	// Dynamically compute U_hot: 90th percentile of all edge loads
	d.Uhot = d.computePercentile(90.0)

	// Compute average latency for each destination using KSP as theta_L(d)
	d.computeThetaLForDestinations(start, validEnds, logger)

	logger.Debug("Computed parameters", slog.String("pre", pre),
		slog.Float64("capacity", d.capacity), slog.Float64("U_hot", d.Uhot),
		slog.Int("flowsPerDest", d.flowsPerDest), slog.Any("thetaLMap", d.thetaLMap))

	logger.Info("FlowOptimizationSolver: parameters calculated dynamically",
		slog.String("pre", pre), slog.Int("numDestinations", len(validEnds)),
		slog.Int("flowsPerDest", d.flowsPerDest), slog.Float64("capacity", d.capacity),
		slog.Float64("U_hot (90th percentile)", d.Uhot))

	// 1. Build residual graph with capacities
	logger.Debug("STEP 1: Building residual graph with capacities", slog.String("pre", pre))
	resGraph := d.buildResidualGraphWithCapacity()
	logger.Debug("Residual graph built", slog.String("pre", pre), slog.Int("nodes", len(resGraph)))

	// 2. Find feasible paths for each destination (satisfy latency constraint and flow limit)
	logger.Info("STEP 2: Finding feasible paths for each destination", slog.String("pre", pre))
	allPaths := list.New()
	destFlowCount := make(map[string]int) // Track flow count per destination
	usedPaths := make(map[string]bool)    // Track used paths to avoid duplicates

	// Helper function to check if path already exists
	pathToString := func(path []string) string {
		return strings.Join(path, "->")
	}

	for _, end := range validEnds {
		logger.Debug("Searching paths to destination", slog.String("pre", pre), slog.String("dest", end))
		// Max flowsPerDest paths per destination
		maxAttempts := d.flowsPerDest * 10 // Limit attempts to prevent infinite loop
		attemptCount := 0

		for i := 0; i < d.flowsPerDest; {
			attemptCount++
			if attemptCount > maxAttempts {
				//logger.Warn("Reached max attempts for destination", slog.String("pre", pre),
				//	slog.String("dest", end), slog.Int("found", i))
				break
			}

			logger.Debug("Finding path", slog.String("pre", pre), slog.String("dest", end),
				slog.Int("pathNum", i+1), slog.Int("attempt", attemptCount))
			pathInfo := d.findFeasiblePathWithLatency(resGraph, start, end, usedPaths, logger, attemptCount)
			if pathInfo == nil {
				logger.Warn("Cannot find enough paths for destination", slog.String("pre", pre),
					slog.String("dest", end), slog.Int("found", i))
				break
			}

			// Check for duplicate paths
			pathStr := pathToString(pathInfo.path)
			if usedPaths[pathStr] {
				//logger.Debug("Skipping duplicate path", slog.String("pre", pre), slog.Any("path", pathInfo.path))
				continue // Don't increment i, keep trying for this slot
			}

			logger.Debug("Path found", slog.String("pre", pre), slog.String("dest", end),
				slog.Int("pathNum", i+1), slog.Any("path", pathInfo.path), slog.Float64("latency", pathInfo.latency))
			allPaths.PushBack(pathInfo)
			usedPaths[pathStr] = true
			destFlowCount[end]++
			//logger.Debug("Allocating flow on this path", slog.String("pre", pre), slog.Any("path", pathInfo.path))
			d.allocateFlow(resGraph, pathInfo.path, 1.0, logger) // Allocate 1 unit of flow
			i++                                                  // Only increment when we successfully add a path
		}

		//Unban all edges after finding all paths for this destination
		//d.unbanAllEdges(resGraph)
	}

	logger.Debug("Initial path search completed", slog.String("pre", pre),
		slog.Int("totalPaths", allPaths.Len()), slog.Any("destFlowCount", destFlowCount))

	if allPaths.Len() == 0 {
		return nil, fmt.Errorf("no feasible path found from %s to any destination", start)
	}

	// 3. Record all historical paths
	pathAccumulated := list.New()
	for e := allPaths.Front(); e != nil; e = e.Next() {
		pathAccumulated.PushBack(e.Value)
	}

	// 4. Initialize optimal solution
	bestPaths := d.copyPathList(allPaths)
	bestScore := d.objective(allPaths, resGraph)

	// Record initial size before removing paths (for maxIter calculation)
	initialSize := allPaths.Len()

	// 5. Remove beta*|S| newest paths and release their flows
	removeNum := int(d.beta * float64(allPaths.Len()))
	logger.Info("STEP 3: Removing beta*|S| newest paths and releasing flows",
		slog.String("pre", pre), slog.Float64("beta", d.beta), slog.Int("removeNum", removeNum))

	for i := 0; i < removeNum && allPaths.Len() > 0; i++ {
		oldestElm := allPaths.Front()
		oldestPathInfo := oldestElm.Value.(*PathWithInfo)
		// Release flow before removing path
		logger.Debug("Releasing flow for removed path", slog.String("pre", pre),
			slog.Int("iteration", i), slog.Any("path", oldestPathInfo.path))
		d.releaseFlow(resGraph, oldestPathInfo.path, 1.0, logger)
		destFlowCount[oldestPathInfo.dest]--
		// Remove from usedPaths so it can be reused in iterations
		delete(usedPaths, pathToString(oldestPathInfo.path))
		allPaths.Remove(oldestElm)
	}

	// 6. Iterative optimization
	// Use initial size for maxIter calculation (alpha * |S_0|), not the reduced size
	maxIter := int(d.alpha * float64(initialSize))
	logger.Info("STEP 4-11: Iterative optimization",
		slog.String("pre", pre), slog.Int("maxIter", maxIter))

	for i := 0; i < maxIter; i++ {
		if allPaths.Len() == 0 {
			break
		}

		logger.Debug("Iteration", slog.String("pre", pre),
			slog.Int("iter", i+1), slog.Int("totalPaths", allPaths.Len()))

		// 7. Remove oldest path and release flow
		oldestElm := allPaths.Front()
		oldestPathInfo := oldestElm.Value.(*PathWithInfo)
		logger.Debug("Removing oldest path", slog.String("pre", pre), slog.Any("path", oldestPathInfo.path))
		allPaths.Remove(oldestElm)
		destFlowCount[oldestPathInfo.dest]--
		// Remove from usedPaths so it can be reused
		delete(usedPaths, pathToString(oldestPathInfo.path))
		d.releaseFlow(resGraph, oldestPathInfo.path, 1.0, logger)

		// 8. Find most frequent path in accumulated paths
		freqPathInfo := d.mostFrequentPathInfo(pathAccumulated)

		// 9. Ban edges of frequent path + first edge of oldest path
		if freqPathInfo != nil {
			logger.Debug("Banning frequent path edges", slog.String("pre", pre), slog.Any("path", freqPathInfo.path))
			d.banPathEdges(resGraph, freqPathInfo.path, logger)
		}
		if len(oldestPathInfo.path) >= 2 {
			logger.Debug("Banning first edge of oldest path", slog.String("pre", pre),
				slog.String("edge", oldestPathInfo.path[0]+"->"+oldestPathInfo.path[1]))
			d.banEdge(resGraph, oldestPathInfo.path[0], oldestPathInfo.path[1], logger)
		}

		// 10. Find new path for random destination (respect flow limit)
		// Shuffle destinations to ensure fair selection (not biased by array order)
		shuffledEnds := make([]string, len(validEnds))
		copy(shuffledEnds, validEnds)
		d.rng.Shuffle(len(shuffledEnds), func(i, j int) {
			shuffledEnds[i], shuffledEnds[j] = shuffledEnds[j], shuffledEnds[i]
		})

		for _, end := range shuffledEnds {
			// Check if destination has reached max flow limit
			if destFlowCount[end] >= d.flowsPerDest {
				continue
			}

			logger.Debug("Searching new path for destination",
				slog.String("pre", pre),
				slog.String("dest", end),
				slog.Int("iter", i+1))
			newPathInfo := d.findFeasiblePathWithLatency(resGraph, start, end, usedPaths, logger, i+1)
			if newPathInfo != nil {
				// Check for duplicate paths
				newPathStr := pathToString(newPathInfo.path)
				if usedPaths[newPathStr] {
					logger.Debug("Skipping duplicate path in iteration",
						slog.String("pre", pre),
						slog.Any("path", newPathInfo.path))
					continue
				}

				logger.Debug("New path found",
					slog.String("pre", pre),
					slog.String("dest", end),
					slog.Any("path", newPathInfo.path))
				allPaths.PushBack(newPathInfo)
				pathAccumulated.PushBack(newPathInfo)
				usedPaths[newPathStr] = true
				destFlowCount[end]++
				d.allocateFlow(resGraph, newPathInfo.path, 1.0, logger)
				break
			}
		}

		// 11. Update optimal solution
		// Critical fix: Evaluate on a stable graph state (after unban)
		// First unban all edges to get a stable state for evaluation
		d.unbanAllEdges(resGraph)

		// Validate current solution against stable graph before evaluation
		validPaths := d.validatePaths(allPaths, resGraph)
		if validPaths.Len() > 0 {
			currentScore := d.objective(validPaths, resGraph)
			logger.Debug("Current score", slog.String("pre", pre), slog.Int("iter", i+1),
				slog.Float64("currentScore", currentScore), slog.Float64("bestScore", bestScore))
			if currentScore > bestScore {
				logger.Info("New best solution found", slog.String("pre", pre),
					slog.Int("iter", i+1), slog.Float64("newScore", currentScore))
				bestPaths = d.copyPathList(validPaths)
				bestScore = currentScore
			}
		}
	}

	// Convert results to PathInfo
	pathInfos := make([]routing.PathInfo, 0, bestPaths.Len())
	for e := bestPaths.Front(); e != nil; e = e.Next() {
		pi := e.Value.(*PathWithInfo)
		pathInfos = append(pathInfos, routing.PathInfo{
			Hops:   pi.path,
			Rtt:    pi.latency,
			RawRTT: pi.latency,
		})
	}

	// Print all selected paths
	logger.Info("FlowOptimizationSolver completed",
		slog.String("pre", pre),
		slog.String("start", start),
		slog.Int("destCount", len(validEnds)),
		slog.Int("totalFlows", len(pathInfos)))

	logger.Debug("=== Final Selected Paths ===", slog.String("pre", pre))
	for i, pathInfo := range pathInfos {
		logger.Debug("Path",
			slog.String("pre", pre),
			slog.Int("index", i+1),
			slog.String("hops", strings.Join(pathInfo.Hops, " -> ")),
			slog.Float64("rtt", pathInfo.Rtt),
			slog.Float64("rawRtt", pathInfo.RawRTT),
			slog.Int("hopsCount", len(pathInfo.Hops)-1))
	}
	logger.Debug("=== End of Paths ===", slog.String("pre", pre))

	return pathInfos, nil
}

func (d *FlowOptimizationSolver) buildResidualGraphWithCapacity() map[string]map[string]*EdgeFlow {
	g := make(map[string]map[string]*EdgeFlow)
	for _, e := range d.edges {
		if g[e.SourceIp] == nil {
			g[e.SourceIp] = make(map[string]*EdgeFlow)
		}
		// Compute theta_a(v) dynamically based on edge's CPU load
		thetaA := d.computeThetaA(e.Load)

		// Log thetaA if it's not equal to 1
		if thetaA != 1.0 {
			logger := slog.Default()
			logger.Info("buildResidualGraphWithCapacity: thetaA != 1",
				slog.String("source", e.SourceIp),
				slog.String("dest", e.DestinationIp),
				slog.Float64("load", e.Load),
				slog.Float64("thetaA", thetaA))
		}
		effCap := thetaA * d.capacity

		ef := &EdgeFlow{
			flow:     0.0,
			cap:      d.capacity,
			thetaA:   thetaA,
			effCap:   effCap,
			banned:   false, // initialize as not banned
			residual: effCap,
		}
		g[e.SourceIp][e.DestinationIp] = ef
	}
	return g
}

// edgeInfo holds edge information for sorting
type edgeInfo struct {
	to      string
	ef      *EdgeFlow
	cost    float64
	isRev   bool
	revFrom string
}

// findFeasiblePathWithLatency finds a feasible path that satisfies latency constraint
// Uses standard residual network with optimized search:
//   - Forward edge: residual = effCap - flow
//   - Backward edge: residual = flow (reverse residual)
func (d *FlowOptimizationSolver) findFeasiblePathWithLatency(g map[string]map[string]*EdgeFlow, s, t string, usedPaths map[string]bool, logger *slog.Logger, attempt int) *PathWithInfo {
	if _, ok := g[s]; !ok {
		return nil
	}

	// Get latency constraint for this destination
	thetaL, ok := d.thetaLMap[t]
	if !ok {
		thetaL = 100.0 // default value
	}

	logger.Debug("findFeasiblePathWithLatency: finding path",
		slog.String("from", s),
		slog.String("to", t),
		slog.Float64("thetaL", thetaL))

	const bannedPenalty = 100.0

	// Collect all edges and sort by cost (latency)
	getSortedEdges := func(u string) []edgeInfo {
		var edges []edgeInfo

		// Forward edges
		for v, ef := range g[u] {
			if ef != nil && ef.residual > 0 {
				cost := d.getEdgeLatency(u, v)
				if ef.banned {
					cost += bannedPenalty
				}
				edges = append(edges, edgeInfo{to: v, ef: ef, cost: cost, isRev: false})
			}
		}

		// Backward edges (reverse residual)
		for v := range nodesInGraph(g) {
			if v != u && g[v] != nil && g[v][u] != nil {
				revEf := g[v][u]
				if revEf.getReverseResidual() > 0 {
					cost := 0.0
					if revEf.banned {
						cost += bannedPenalty
					}
					edges = append(edges, edgeInfo{to: v, ef: revEf, cost: cost, isRev: true, revFrom: u})
				}
			}
		}

		// Sort by cost (ascending)
		for i := 0; i < len(edges)-1; i++ {
			for j := i + 1; j < len(edges); j++ {
				if edges[j].cost < edges[i].cost {
					edges[i], edges[j] = edges[j], edges[i]
				}
			}
		}

		return edges
	}

	// Find feasible paths using DFS, returning the first valid path found
	// Use different edge ordering strategies based on attempt to find diverse paths
	var foundPath []string
	var foundLatency float64 = 0

	// For first two attempts, find the shortest path (excluding previously selected paths)
	if attempt <= 1 {
		var bestPath []string
		var bestLatency float64 = thetaL + 1

		visited := make(map[string]bool)
		path := []string{}

		var dfs func(u string, currentLatency float64)
		dfs = func(u string, currentLatency float64) {
			if len(path) >= maxHops {
				return
			}

			if u == t {
				if currentLatency < bestLatency && currentLatency <= thetaL {
					// Check if this path is different from previously selected paths
					tempPath := make([]string, len(path)+1)
					copy(tempPath, path)
					tempPath[len(path)] = u

					pathStr := strings.Join(tempPath, "->")
					if !usedPaths[pathStr] {
						bestLatency = currentLatency
						bestPath = tempPath
					}
				}
				return
			}

			visited[u] = true
			path = append(path, u)

			sortedEdges := getSortedEdges(u)

			for _, ei := range sortedEdges {
				if visited[ei.to] {
					continue
				}

				if currentLatency+ei.cost >= bestLatency {
					continue
				}

				if currentLatency+ei.cost > thetaL {
					continue
				}

				dfs(ei.to, currentLatency+ei.cost)
			}

			path = path[:len(path)-1]
			visited[u] = false
		}

		dfs(s, 0.0)

		if bestPath != nil {
			foundPath = bestPath
			foundLatency = bestLatency
		}
	} else {
		// For subsequent attempts, use random ordering to find different paths
		visited := make(map[string]bool)
		path := []string{}

		var dfs func(u string, currentLatency float64) bool
		dfs = func(u string, currentLatency float64) bool {
			if len(path) >= maxHops {
				return false
			}

			if u == t {
				if currentLatency <= thetaL {
					foundPath = make([]string, len(path)+1)
					copy(foundPath, path)
					foundPath[len(path)] = u
					foundLatency = currentLatency
					return true
				}
				return false
			}

			visited[u] = true
			path = append(path, u)

			sortedEdges := getSortedEdges(u)

			// Shuffle edges to explore different paths
			for j := len(sortedEdges) - 1; j > 0; j-- {
				k := rand.Intn(j + 1)
				sortedEdges[j], sortedEdges[k] = sortedEdges[k], sortedEdges[j]
			}

			for _, ei := range sortedEdges {
				if visited[ei.to] {
					continue
				}

				if currentLatency+ei.cost > thetaL {
					continue
				}

				if dfs(ei.to, currentLatency+ei.cost) {
					return true
				}
			}

			path = path[:len(path)-1]
			visited[u] = false
			return false
		}

		dfs(s, 0.0)
	}

	if foundPath != nil {
		logger.Debug("Path found",
			slog.String("from", s),
			slog.String("to", t),
			slog.Any("path", foundPath),
			slog.Float64("latency", foundLatency),
			slog.Int("hops", len(foundPath)-1),
			slog.Int("attempt", attempt))
		return &PathWithInfo{
			path:    foundPath,
			dest:    t,
			latency: foundLatency,
		}
	}

	logger.Debug("No feasible path found within constraints",
		slog.String("from", s),
		slog.String("to", t),
		slog.Float64("thetaL", thetaL))
	return nil
}

// nodesInGraph returns all unique nodes in the graph
func nodesInGraph(g map[string]map[string]*EdgeFlow) map[string]bool {
	nodes := make(map[string]bool)
	for from, edges := range g {
		nodes[from] = true
		for to := range edges {
			nodes[to] = true
		}
	}
	return nodes
}

// getEdgeLatency gets the latency of an edge (O(1) lookup using latencyMap)
func (d *FlowOptimizationSolver) getEdgeLatency(from, to string) float64 {
	key := from + "->" + to
	if latency, ok := d.latencyMap[key]; ok {
		return latency
	}
	// Return infinity if edge not found - this will cause latency constraint to fail
	// This prevents silent failures where missing edges get a fake latency value
	return math.MaxFloat64
}

// allocateFlow allocates flow along a path
// Standard residual network update:
//   - Forward edge: flow += f, residual = effCap - flow
//   - Backward residual is implicitly flow (no need to store separately)
func (d *FlowOptimizationSolver) allocateFlow(g map[string]map[string]*EdgeFlow, path []string, flow float64, logger *slog.Logger) {
	//logger.Debug("allocateFlow: allocating flow", slog.Any("path", path), slog.Float64("flow", flow))
	for i := 0; i < len(path)-1; i++ {
		from := path[i]
		to := path[i+1]
		if g[from] != nil && g[from][to] != nil {
			oldFlow := g[from][to].flow
			oldResidual := g[from][to].residual
			g[from][to].flow += flow
			g[from][to].updateResidual()
			logger.Debug("Edge updated", slog.String("edge", from+"->"+to),
				slog.Float64("oldFlow", oldFlow), slog.Float64("newFlow", g[from][to].flow),
				slog.Float64("oldResidual", oldResidual), slog.Float64("newResidual", g[from][to].residual))
		}
	}
}

// releaseFlow releases flow along a path
// Standard residual network update - reverses allocateFlow
func (d *FlowOptimizationSolver) releaseFlow(g map[string]map[string]*EdgeFlow, path []string, flow float64, logger *slog.Logger) {
	//logger.Debug("releaseFlow: releasing flow", slog.Any("path", path), slog.Float64("flow", flow))
	for i := 0; i < len(path)-1; i++ {
		from := path[i]
		to := path[i+1]
		if g[from] != nil && g[from][to] != nil {
			oldFlow := g[from][to].flow
			oldResidual := g[from][to].residual
			g[from][to].flow -= flow
			if g[from][to].flow < 0 {
				g[from][to].flow = 0
			}
			g[from][to].updateResidual()
			logger.Debug("Edge updated", slog.String("edge", from+"->"+to),
				slog.Float64("oldFlow", oldFlow), slog.Float64("newFlow", g[from][to].flow),
				slog.Float64("oldResidual", oldResidual), slog.Float64("newResidual", g[from][to].residual))
		}
	}
}

// validatePaths validates paths against the current residual graph state
// Returns only paths that are still feasible (all edges have sufficient residual capacity)
func (d *FlowOptimizationSolver) validatePaths(paths *list.List, g map[string]map[string]*EdgeFlow) *list.List {
	valid := list.New()
	for e := paths.Front(); e != nil; e = e.Next() {
		pi := e.Value.(*PathWithInfo)
		if d.isPathFeasible(pi.path, g) {
			valid.PushBack(pi)
		}
	}
	return valid
}

// isPathFeasible checks if a path is feasible in the current graph state
func (d *FlowOptimizationSolver) isPathFeasible(path []string, g map[string]map[string]*EdgeFlow) bool {
	for i := 0; i < len(path)-1; i++ {
		from := path[i]
		to := path[i+1]
		if g[from] == nil || g[from][to] == nil {
			return false
		}
		ef := g[from][to]
		if ef.banned || ef.residual <= 0 {
			return false
		}
	}
	return true
}

// banPathEdges temporarily bans all edges in a path (for exploration)
func (d *FlowOptimizationSolver) banPathEdges(g map[string]map[string]*EdgeFlow, path []string, logger *slog.Logger) {
	//logger.Debug("banPathEdges: banning all edges in path", slog.Any("path", path))
	for i := 0; i < len(path)-1; i++ {
		d.banEdge(g, path[i], path[i+1], logger)
	}
}

// banEdge temporarily bans a single edge (sets banned flag instead of removing)
func (d *FlowOptimizationSolver) banEdge(g map[string]map[string]*EdgeFlow, from, to string, logger *slog.Logger) {
	if g[from] != nil && g[from][to] != nil {
		g[from][to].banned = true
		//logger.Debug("Banned edge", slog.String("from", from), slog.String("to", to))
	}
}

// unbanAllEdges unbans all edges in the graph (called at the end of each iteration)
func (d *FlowOptimizationSolver) unbanAllEdges(g map[string]map[string]*EdgeFlow) {
	for _, edges := range g {
		for _, ef := range edges {
			if ef != nil {
				ef.banned = false
			}
		}
	}
}

// allDestinationsFull checks if all destinations have reached max flowsPerDest
func (d *FlowOptimizationSolver) allDestinationsFull(destFlowCount map[string]int, destinations []string) bool {
	for _, dest := range destinations {
		if destFlowCount[dest] < d.flowsPerDest {
			return false
		}
	}
	return true
}

// objective function: maximize path count + diversity - latency
// Score = path_count * pathWeight + diversity_score * diversityWeight - latency_penalty
// Uses current residual graph state to compute actual path costs
func (d *FlowOptimizationSolver) objective(lst *list.List, g map[string]map[string]*EdgeFlow) float64 {
	if lst.Len() == 0 {
		return 0.0
	}

	// Weights for balancing objectives
	// pathWeight should be dominant to prioritize max F (paper objective)
	const pathWeight = 10.0
	const diversityWeight = 1.0
	const latencyWeight = 0.1
	const congestionWeight = 1.0 // penalty for using congested edges

	// Count edge usage for diversity calculation
	edgeUsage := make(map[string]int)
	totalLatency := 0.0
	totalCongestion := 0.0

	for e := lst.Front(); e != nil; e = e.Next() {
		pi := e.Value.(*PathWithInfo)

		// Recompute latency from current graph state (not cached value)
		pathLatency := d.computePathLatency(pi.path, g)
		totalLatency += pathLatency

		// Count each edge in the path and compute congestion penalty
		path := pi.path
		for i := 0; i < len(path)-1; i++ {
			from := path[i]
			to := path[i+1]
			edgeKey := from + "->" + to
			edgeUsage[edgeKey]++

			// Congestion penalty: based on current residual capacity
			if g[from] != nil && g[from][to] != nil {
				ef := g[from][to]
				// Congestion = 1 - (residual / effCap) -> 0 = uncongested, 1 = fully congested
				if ef.effCap > 0 {
					congestion := 1.0 - (ef.residual / ef.effCap)
					totalCongestion += congestion
				}
			}
		}
	}

	// Diversity score: edges used fewer times contribute more
	// 1/count for each edge, so shared edges reduce score
	diversityScore := 0.0
	for _, count := range edgeUsage {
		diversityScore += 1.0 / float64(count)
	}

	// Combine metrics with weights
	// pathCount is dominant (10x weight) to prioritize max F as per paper
	pathCount := float64(lst.Len())
	latencyPenalty := totalLatency * latencyWeight
	congestionPenalty := totalCongestion * congestionWeight

	// Total score favors: more paths (primary), more diversity (secondary),
	// lower latency (tertiary), lower congestion (quaternary)
	return pathCount*pathWeight + diversityScore*diversityWeight - latencyPenalty - congestionPenalty
}

// computePathLatency computes latency of a path using current graph state
func (d *FlowOptimizationSolver) computePathLatency(path []string, g map[string]map[string]*EdgeFlow) float64 {
	latency := 0.0
	for i := 0; i < len(path)-1; i++ {
		from := path[i]
		to := path[i+1]
		// Use edge latency from latencyMap (original edge property)
		// This is more stable than using dynamic residual-based weights
		latency += d.getEdgeLatency(from, to)
	}
	return latency
}

// copyPathList copies a path list
func (d *FlowOptimizationSolver) copyPathList(l *list.List) *list.List {
	nl := list.New()
	for e := l.Front(); e != nil; e = e.Next() {
		pi := e.Value.(*PathWithInfo)
		nl.PushBack(&PathWithInfo{
			path:    pi.path,
			dest:    pi.dest,
			latency: pi.latency,
		})
	}
	return nl
}

// mostFrequentPathInfo counts path frequencies and returns the most frequent path
func (d *FlowOptimizationSolver) mostFrequentPathInfo(lst *list.List) *PathWithInfo {
	cnt := make(map[string]int)
	maxCnt := 0
	var bestPath *PathWithInfo

	for e := lst.Front(); e != nil; e = e.Next() {
		pi := e.Value.(*PathWithInfo)
		key := ""
		for _, s := range pi.path {
			key += s + "|"
		}
		cnt[key]++
		if cnt[key] > maxCnt {
			maxCnt = cnt[key]
			bestPath = pi
		}
	}
	return bestPath
}

// Computing single destination interface (compatible with legacy interface)
func (d *FlowOptimizationSolver) Computing(start, end string, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	logger.Info("Computing START",
		slog.String("pre", pre),
		slog.String("start", start),
		slog.String("end", end))

	// Validate source and destination
	nodes := make(map[string]struct{})
	for _, e := range d.edges {
		nodes[e.SourceIp] = struct{}{}
		nodes[e.DestinationIp] = struct{}{}
	}

	if _, ok := nodes[start]; !ok {
		return nil, fmt.Errorf("start node %s not found in graph", start)
	}
	if _, ok := nodes[end]; !ok {
		return nil, fmt.Errorf("end node %s not found in graph", end)
	}

	// Build residual graph
	resGraph := d.buildResidualGraphWithCapacity()

	// Find feasible path
	usedPaths := make(map[string]bool)
	pathInfo := d.findFeasiblePathWithLatency(resGraph, start, end, usedPaths, logger, 1)
	if pathInfo == nil {
		return nil, fmt.Errorf("no feasible path found from %s to %s", start, end)
	}

	// Convert to PathInfo
	return []routing.PathInfo{
		{
			Hops:   pathInfo.path,
			Rtt:    pathInfo.latency,
			RawRTT: pathInfo.latency,
		},
	}, nil
}
