package core_domain

import (
	"container/list"
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
)

// FlowOptimizationSolver implements the flow optimization problem
// max F s.t.
// 1. Flow conservation: sum f_ij = sum f_ji for all i
// 2. Capacity constraint: f_ij <= theta_a(v) * u_ij, where theta_a(v) = 1 - U'_v is dynamically computed based on CPU utilization
// 3. Latency constraint: L(P) <= theta_L(d), each destination d has its own latency bound
// 4. From s to N destinations, flowsPerDest flows per destination
type FlowOptimizationSolver struct {
	edges        []*graph.Edge
	alpha        float64 // iteration count coefficient
	beta         float64 // path removal ratio
	capacity     float64 // edge capacity u_ij (dynamically computed: numDestinations * flowsPerDest)
	flowsPerDest int     // number of flows per destination
	// CPU utilization parameters
	Tu      float64 // safe utilization threshold (node is uncongested below this value)
	Uhot    float64 // high-percentile utilization threshold (hotspot condition)
	epsilon float64 // small constant to prevent division by zero
	// Latency constraints (per-destination)
	thetaLMap map[string]float64 // destination -> latency constraint
	kspK      int                // number of KSP paths used to compute average latency
}

// NewFlowOptimizationSolver creates a new instance
func NewFlowOptimizationSolver(edges []*graph.Edge) *FlowOptimizationSolver {
	return &FlowOptimizationSolver{
		edges:        edges,
		alpha:        2.0, // iteration count coefficient (increase for more thorough optimization)
		beta:         0.9, // path removal ratio (aggressive exploration: remove 90% of initial paths)
		capacity:     0.0, // edge capacity computed dynamically in ComputingMulti based on destination count
		flowsPerDest: 3,   // 3 flows per destination
		// CPU utilization parameters
		Tu:      60.0,  // safe utilization threshold (node is uncongested below this value)
		Uhot:    0.0,   // high-percentile utilization threshold (computed as 90th percentile at runtime)
		epsilon: 0.001, // small constant to prevent division by zero
		// Latency constraint parameters
		thetaLMap: make(map[string]float64),
		kspK:      5, // use top 5 KSP paths to compute average latency
	}
}

// EdgeFlow records flow information for an edge
type EdgeFlow struct {
	flow   float64 // current flow
	cap    float64 // original capacity u_ij
	thetaA float64 // dynamic utilization factor [0,1]
	effCap float64 // effective capacity = thetaA * cap
}

// computeThetaA computes the dynamic scaling factor theta_a(v) based on CPU utilization
// Formula: U'_v = min(1, max(U_v-Tu,0) / max(U_hot-Tu, epsilon))
//
//	theta_a(v) = 1 - U'_v
func (d *FlowOptimizationSolver) computeThetaA(cpuLoad float64) float64 {
	numerator := max(cpuLoad-d.Tu, 0.0)
	denominator := max(d.Uhot-d.Tu, d.epsilon)

	U_prime := numerator / denominator
	U_prime = min(U_prime, 1.0)

	return 1.0 - U_prime
}

// max returns the larger value
func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// min returns the smaller value
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
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

		// Compute average latency
		totalLatency := 0.0
		for _, path := range paths {
			totalLatency += path.Rtt
		}
		avgLatency := totalLatency / float64(len(paths))

		// Set latency constraint to average latency (can multiply by a factor)
		d.thetaLMap[end] = avgLatency

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
	if len(ends) == 0 {
		return nil, fmt.Errorf("no destinations provided")
	}

	// Dynamically compute edge capacity: capacity = numDestinations * flowsPerDest
	d.capacity = float64(len(ends) * d.flowsPerDest)

	// Dynamically compute U_hot: 90th percentile of all edge loads
	d.Uhot = d.computePercentile(90.0)

	// Compute average latency for each destination using KSP as theta_L(d)
	d.computeThetaLForDestinations(start, ends, logger)

	logger.Info("FlowOptimizationSolver: parameters calculated dynamically",
		slog.String("pre", pre),
		slog.Int("numDestinations", len(ends)),
		slog.Int("flowsPerDest", d.flowsPerDest),
		slog.Float64("capacity", d.capacity),
		slog.Float64("U_hot (90th percentile)", d.Uhot))

	// 1. Build residual graph with capacities
	resGraph := d.buildResidualGraphWithCapacity()

	// 2. Find feasible paths for each destination (satisfy latency constraint and flow limit)
	allPaths := list.New()
	destFlowCount := make(map[string]int) // Track flow count per destination

	for _, end := range ends {
		// Max flowsPerDest paths per destination
		for i := 0; i < d.flowsPerDest; i++ {
			pathInfo := d.findFeasiblePathWithLatency(resGraph, start, end)
			if pathInfo == nil {
				logger.Warn("Cannot find enough paths for destination",
					slog.String("dest", end), slog.Int("found", i))
				break
			}
			allPaths.PushBack(pathInfo)
			destFlowCount[end]++
			d.allocateFlow(resGraph, pathInfo.path, 1.0) // Allocate 1 unit of flow
		}
	}

	if allPaths.Len() == 0 {
		return nil, fmt.Errorf("no feasible path found from %s to any destination", start)
	}

	// 3. Record all historical paths
	P := list.New()
	for e := allPaths.Front(); e != nil; e = e.Next() {
		P.PushBack(e.Value)
	}

	// 4. Initialize optimal solution
	bestPaths := d.copyPathList(allPaths)
	bestScore := d.objective(allPaths)

	// 5. Remove beta*|S| newest paths
	removeNum := int(d.beta * float64(allPaths.Len()))
	for i := 0; i < removeNum && allPaths.Len() > 0; i++ {
		oldestElm := allPaths.Front()
		oldestPathInfo := oldestElm.Value.(*PathWithInfo)
		destFlowCount[oldestPathInfo.dest]--
		allPaths.Remove(oldestElm)
	}

	// 6. Iterative optimization
	maxIter := int(d.alpha * float64(allPaths.Len()))
	for i := 0; i < maxIter; i++ {
		if allPaths.Len() == 0 {
			break
		}

		// 7. Remove oldest path and release flow
		oldestElm := allPaths.Front()
		oldestPathInfo := oldestElm.Value.(*PathWithInfo)
		allPaths.Remove(oldestElm)
		destFlowCount[oldestPathInfo.dest]--
		d.releaseFlow(resGraph, oldestPathInfo.path, 1.0)

		// 8. Find most frequent path
		freqPathInfo := d.mostFrequentPathInfo(P)

		// 9. Ban edges of frequent path + first edge of oldest path
		if freqPathInfo != nil {
			d.banPathEdges(resGraph, freqPathInfo.path)
		}
		if len(oldestPathInfo.path) >= 2 {
			d.banEdge(resGraph, oldestPathInfo.path[0], oldestPathInfo.path[1])
		}

		// 10. Find new path for random destination (respect flow limit)
		for _, end := range ends {
			// Check if destination has reached max flow limit
			if destFlowCount[end] >= d.flowsPerDest {
				continue
			}

			newPathInfo := d.findFeasiblePathWithLatency(resGraph, start, end)
			if newPathInfo != nil {
				allPaths.PushBack(newPathInfo)
				P.PushBack(newPathInfo)
				destFlowCount[end]++
				d.allocateFlow(resGraph, newPathInfo.path, 1.0)
				break
			}
		}

		// 11. 更新最优解
		currentScore := d.objective(allPaths)
		if currentScore > bestScore {
			bestPaths = d.copyPathList(allPaths)
			bestScore = currentScore
		}
	}

	// 转换结果为 PathInfo
	pathInfos := make([]routing.PathInfo, 0, bestPaths.Len())
	for e := bestPaths.Front(); e != nil; e = e.Next() {
		pi := e.Value.(*PathWithInfo)
		pathInfos = append(pathInfos, routing.PathInfo{
			Hops:   pi.path,
			Rtt:    pi.latency,
			RawRTT: pi.latency,
		})
	}

	logger.Info("FlowOptimizationSolver completed",
		slog.String("pre", pre),
		slog.String("start", start),
		slog.Int("destCount", len(ends)),
		slog.Int("totalFlows", len(pathInfos)))

	return pathInfos, nil
}

// buildResidualGraphWithCapacity builds a residual graph with capacities (using dynamic theta_a(v))
func (d *FlowOptimizationSolver) buildResidualGraphWithCapacity() map[string]map[string]*EdgeFlow {
	g := make(map[string]map[string]*EdgeFlow)
	for _, e := range d.edges {
		if g[e.SourceIp] == nil {
			g[e.SourceIp] = make(map[string]*EdgeFlow)
		}
		// Compute theta_a(v) dynamically based on edge's CPU load
		thetaA := d.computeThetaA(e.Load)
		effCap := thetaA * d.capacity

		g[e.SourceIp][e.DestinationIp] = &EdgeFlow{
			flow:   0.0,
			cap:    d.capacity,
			thetaA: thetaA,
			effCap: effCap,
		}
	}
	return g
}

const maxHops = 10 // maximum hop limit

// findFeasiblePathWithLatency finds a feasible path that satisfies latency constraint
func (d *FlowOptimizationSolver) findFeasiblePathWithLatency(g map[string]map[string]*EdgeFlow, s, t string) *PathWithInfo {
	if _, ok := g[s]; !ok {
		return nil
	}

	// Get latency constraint for this destination
	thetaL, ok := d.thetaLMap[t]
	if !ok {
		thetaL = 100.0 // default value
	}

	visited := make(map[string]bool)
	path := []string{}
	totalLatency := 0.0

	var dfs func(u string) bool
	dfs = func(u string) bool {
		// Hop pruning: stop if current path length (hops = len(path)) exceeds maxHops
		if len(path) >= maxHops {
			return false
		}

		if u == t {
			path = append(path, u)
			return true
		}
		visited[u] = true
		path = append(path, u)

		for v, ef := range g[u] {
			if !visited[v] && ef != nil && ef.flow < ef.effCap {
				// Estimate latency (using edge's Latency property)
				edgeLatency := d.getEdgeLatency(u, v)
				if totalLatency+edgeLatency <= thetaL {
					totalLatency += edgeLatency
					if dfs(v) {
						return true
					}
					totalLatency -= edgeLatency
				}
			}
		}

		path = path[:len(path)-1]
		return false
	}

	if dfs(s) {
		return &PathWithInfo{
			path:    path,
			dest:    t,
			latency: totalLatency,
		}
	}
	return nil
}

// getEdgeLatency gets the latency of an edge
func (d *FlowOptimizationSolver) getEdgeLatency(from, to string) float64 {
	for _, e := range d.edges {
		if e.SourceIp == from && e.DestinationIp == to {
			return e.Latency
		}
	}
	return 10.0 // default latency
}

// allocateFlow allocates flow along a path
func (d *FlowOptimizationSolver) allocateFlow(g map[string]map[string]*EdgeFlow, path []string, flow float64) {
	for i := 0; i < len(path)-1; i++ {
		from := path[i]
		to := path[i+1]
		if g[from] != nil && g[from][to] != nil {
			g[from][to].flow += flow
		}
	}
}

// releaseFlow releases flow along a path
func (d *FlowOptimizationSolver) releaseFlow(g map[string]map[string]*EdgeFlow, path []string, flow float64) {
	for i := 0; i < len(path)-1; i++ {
		from := path[i]
		to := path[i+1]
		if g[from] != nil && g[from][to] != nil {
			g[from][to].flow -= flow
			if g[from][to].flow < 0 {
				g[from][to].flow = 0
			}
		}
	}
}

// banPathEdges bans all edges in a path
func (d *FlowOptimizationSolver) banPathEdges(g map[string]map[string]*EdgeFlow, path []string) {
	for i := 0; i < len(path)-1; i++ {
		d.banEdge(g, path[i], path[i+1])
	}
}

// banEdge bans a single edge
func (d *FlowOptimizationSolver) banEdge(g map[string]map[string]*EdgeFlow, from, to string) {
	if g[from] != nil {
		g[from][to] = nil
	}
}

// objective function: maximize path count (feasible flow count)
func (d *FlowOptimizationSolver) objective(lst *list.List) int {
	return lst.Len()
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
	return d.ComputingMulti(start, []string{end}, pre, logger)
}

// ComputeMultiDestination package-level multi-destination flow optimization function (following onewan-multi.go style)
func ComputeMultiDestination(solver *FlowOptimizationSolver, start string, ends []string, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	if len(ends) == 0 {
		return solver.Computing(start, "", pre, logger)
	}

	if len(ends) == 1 {
		return solver.Computing(start, ends[0], pre, logger)
	}

	// 验证图中存在源节点
	nodes := make(map[string]struct{})
	for _, e := range solver.edges {
		nodes[e.SourceIp] = struct{}{}
		nodes[e.DestinationIp] = struct{}{}
	}

	if _, ok := nodes[start]; !ok {
		return nil, fmt.Errorf("start node %s not found in graph", start)
	}

	// 验证所有目的地都在图中
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

	// 调用 solver 的多目的地方法
	return solver.ComputingMulti(start, validEnds, pre, logger)
}
