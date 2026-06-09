package core_domain

import (
	"container/heap"
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
	"math"
	"strings"
)

type KShortestSolver struct {
	edges      []*graph.Edge
	alpha      float64
	k          int
	useLatency bool // If true, use Latency instead of EdgeWeight for path calculation
}

func NewKShortestSolver(edges []*graph.Edge, k int) *KShortestSolver {
	return NewKShortestSolverWithLatency(edges, k, false)
}

// NewKShortestSolverWithLatency creates a solver with option to use Latency instead of EdgeWeight
func NewKShortestSolverWithLatency(edges []*graph.Edge, k int, useLatency bool) *KShortestSolver {
	var g []*graph.Edge
	for _, e := range edges {
		g = append(g, e)
	}
	// Set k=5 by default for path selection
	if k <= 0 {
		k = 5
	}
	return &KShortestSolver{
		edges:      g,
		alpha:      1,
		k:          k,
		useLatency: useLatency,
	}
}

func (ks *KShortestSolver) Computing(start, end, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	// Build graph structure
	graph_ := make(map[string][]*graph.Edge)
	nodes := make(map[string]struct{})
	for _, e := range ks.edges {
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

	// Use Yen's algorithm to find top k shortest paths (k=5)
	paths, err := ks.yensAlgorithm(start, end, graph_, logger)
	if err != nil {
		return nil, err
	}

	// Convert to PathInfo format
	var pathInfos []routing.PathInfo
	for _, path := range paths {
		pathInfos = append(pathInfos, routing.PathInfo{
			Hops: path.hops,
			Rtt:  path.cost,
		})
	}

	logger.Info("K-shortest paths found", slog.String("pre", pre),
		slog.String("start", start), slog.String("end", end),
		slog.Int("k", ks.k), slog.Int("selected", len(pathInfos)),
		slog.Any("paths", pathInfos))

	return pathInfos, nil
}

type Path struct {
	hops   []string
	cost   float64
	rawRTT float64
}

func (ks *KShortestSolver) yensAlgorithm(start, end string, graph_ map[string][]*graph.Edge, logger *slog.Logger) ([]Path, error) {
	// Initialize result list
	var A []Path
	// Initialize candidate path list
	var B PriorityQueue

	// Find first shortest path
	firstPath, err := ks.findShortestPath(start, end, graph_, nil, nil, logger)
	if err != nil {
		return nil, err
	}
	A = append(A, *firstPath)

	// Continue until we have k paths or no more candidates
	for len(A) < ks.k {
		// Process all paths in A to generate candidates
		for _, prevPath := range A {
			for j := 0; j < len(prevPath.hops)-1; j++ {
				// Build spur node and root path
				spurNode := prevPath.hops[j]
				rootPath := prevPath.hops[:j+1]

				// Check if spurNode has outgoing edges
				if _, ok := graph_[spurNode]; !ok {
					continue
				}

				// Create forbidden nodes set from root path (except spur node)
				forbiddenNodes := make(map[string]bool)
				for _, node := range rootPath[:len(rootPath)-1] {
					forbiddenNodes[node] = true
				}

				// Remove edges related to root path
				removedEdges := make([]*graph.Edge, 0)
				for _, edge := range graph_[spurNode] {
					if !ks.isInPath(edge.DestinationIp, rootPath) {
						removedEdges = append(removedEdges, edge)
					}
				}
				// Temporarily remove these edges
				originalEdges := graph_[spurNode]
				graph_[spurNode] = removedEdges

				// Remove edges from spur node to next node in all found paths with same prefix
				for _, path := range A {
					if len(path.hops) > j && ks.pathsSharePrefix(path.hops, rootPath) {
						// Remove edge from spur node to path[j+1]
						if j+1 < len(path.hops) {
							target := path.hops[j+1]
							newEdges := make([]*graph.Edge, 0)
							for _, edge := range graph_[spurNode] {
								if edge.DestinationIp != target {
									newEdges = append(newEdges, edge)
								}
							}
							graph_[spurNode] = newEdges
						}
					}
				}

				// Skip if spur node is already the destination
				if spurNode == end {
					graph_[spurNode] = originalEdges
					continue
				}

				// Skip if root path already contains the destination
				if ks.isInPath(end, rootPath) {
					graph_[spurNode] = originalEdges
					continue
				}

				// Find shortest path from spur node to end, avoiding root path nodes
				spurPath, err := ks.findShortestPath(spurNode, end, graph_, forbiddenNodes, nil, logger)
				if err == nil {
					// Calculate root path cost and rawRTT
					rootCost := ks.calculatePathCost(rootPath, graph_)
					rootRTT := ks.calculatePathRTT(rootPath, graph_)
					// Build complete path
					completePath := Path{
						hops:   append(rootPath[:len(rootPath)-1], spurPath.hops...),
						cost:   rootCost + spurPath.cost,
						rawRTT: rootRTT + spurPath.rawRTT,
					}
					// Check if path is valid (no duplicate nodes)
					if !ks.hasDuplicateNodes(completePath.hops) {
						// Check if path is already in result list or candidate list
						if !ks.isPathInList(completePath, A) && !ks.isPathInQueue(completePath, &B) {
							heap.Push(&B, &PQNode{
								node:   strings.Join(completePath.hops, "->"),
								cost:   completePath.cost,
								rawRTT: completePath.rawRTT,
							})
						}
					}
				}

				// Restore original edges
				graph_[spurNode] = originalEdges
			}
		}

		if B.Len() == 0 {
			// No more candidate paths
			break
		}

		// Select shortest path from candidate list
		shortest := heap.Pop(&B).(*PQNode)
		if shortest == nil {
			break
		}
		// Parse path
		hops := strings.Split(shortest.node, "->")

		// Check if this path already exists in A
		newPath := Path{hops: hops, cost: shortest.cost, rawRTT: shortest.rawRTT}
		if !ks.isPathInList(newPath, A) {
			A = append(A, newPath)
		}
	}

	// Sort results by cost (Latency) before returning to ensure correct ordering
	for i := 0; i < len(A)-1; i++ {
		for j := i + 1; j < len(A); j++ {
			if A[j].cost < A[i].cost {
				A[i], A[j] = A[j], A[i]
			}
		}
	}

	return A, nil
}

func (ks *KShortestSolver) hasDuplicateNodes(hops []string) bool {
	seen := make(map[string]bool)
	for _, hop := range hops {
		if seen[hop] {
			return true
		}
		seen[hop] = true
	}
	return false
}

func (ks *KShortestSolver) findShortestPath(start, end string, graph_ map[string][]*graph.Edge,
	forbiddenNodes map[string]bool, forbiddenEdges map[string]bool, logger *slog.Logger) (*Path, error) {

	dist := make(map[string]float64)
	prev := make(map[string]string)
	rawRTT := make(map[string]float64)
	// Initialize all nodes in graph (including destination-only nodes)
	allNodes := make(map[string]bool)
	for node := range graph_ {
		dist[node] = math.Inf(1)
		rawRTT[node] = math.Inf(1)
		allNodes[node] = true
	}
	// Also add destination nodes that may not have outgoing edges
	for _, edges := range graph_ {
		for _, e := range edges {
			if !allNodes[e.DestinationIp] {
				dist[e.DestinationIp] = math.Inf(1)
				rawRTT[e.DestinationIp] = math.Inf(1)
				allNodes[e.DestinationIp] = true
			}
		}
	}
	dist[start] = 0
	rawRTT[start] = 0

	pq := &PriorityQueue{}
	heap.Init(pq)
	heap.Push(pq, &PQNode{
		node: start,
		cost: 0,
	})

	for pq.Len() > 0 {
		u := heap.Pop(pq).(*PQNode)
		currNode := u.node
		currCost := u.cost

		if currCost > dist[currNode] {
			continue
		}

		if currNode == end {
			// Build path
			var path []string
			for node := end; node != ""; node = prev[node] {
				path = append([]string{node}, path...)
			}
			return &Path{
				hops:   path,
				cost:   currCost,
				rawRTT: rawRTT[end],
			}, nil
		}

		// Skip processing neighbors if we've already found a path to the destination
		if dist[end] != math.Inf(1) {
			continue
		}

		for _, e := range graph_[currNode] {
			// Check if node or edge is forbidden
			if forbiddenNodes != nil && forbiddenNodes[e.DestinationIp] {
				continue
			}
			edgeKey := currNode + "->" + e.DestinationIp
			if forbiddenEdges != nil && forbiddenEdges[edgeKey] {
				continue
			}

			nextNode := e.DestinationIp
			var newCost float64
			if ks.useLatency {
				// Use pure Latency for path calculation
				newCost = currCost + e.Latency*ks.alpha
			} else {
				// Use EdgeWeight * alpha for path calculation
				newCost = currCost + e.EdgeWeight*ks.alpha
			}
			newRTT := rawRTT[currNode] + e.Latency

			if newCost < dist[nextNode] {
				dist[nextNode] = newCost
				rawRTT[nextNode] = newRTT
				prev[nextNode] = currNode
				heap.Push(pq, &PQNode{
					node: nextNode,
					cost: newCost,
				})
			}
		}
	}

	return nil, fmt.Errorf("no path found from %s to %s", start, end)
}

func (ks *KShortestSolver) isInPath(node string, path []string) bool {
	for _, n := range path {
		if n == node {
			return true
		}
	}
	return false
}

func (ks *KShortestSolver) pathsSharePrefix(path1, path2 []string) bool {
	if len(path1) < len(path2) {
		path1, path2 = path2, path1
	}
	for i := range path2 {
		if path1[i] != path2[i] {
			return false
		}
	}
	return true
}

func (ks *KShortestSolver) isPathInQueue(path Path, pq *PriorityQueue) bool {
	pathStr := strings.Join(path.hops, "->")
	for _, item := range *pq {
		if item.node == pathStr {
			return true
		}
	}
	return false
}

func (ks *KShortestSolver) isPathInList(path Path, list []Path) bool {
	pathStr := strings.Join(path.hops, "->")
	for _, p := range list {
		if strings.Join(p.hops, "->") == pathStr {
			return true
		}
	}
	return false
}

func (ks *KShortestSolver) selectTopPaths(paths []Path, count int, logger *slog.Logger) []Path {
	if len(paths) <= count {
		return paths
	}

	// Separate paths into two groups
	var shortPaths []Path // len(hops) <= 4
	var longPaths []Path  // len(hops) > 4

	for _, path := range paths {
		if len(path.hops) <= 4 {
			shortPaths = append(shortPaths, path)
		} else {
			longPaths = append(longPaths, path)
		}
	}

	// Sort short paths by rawRTT (real latency)
	for i := 0; i < len(shortPaths)-1; i++ {
		for j := i + 1; j < len(shortPaths); j++ {
			if shortPaths[j].rawRTT < shortPaths[i].rawRTT {
				shortPaths[i], shortPaths[j] = shortPaths[j], shortPaths[i]
			}
		}
	}

	// Sort long paths by rawRTT (real latency)
	for i := 0; i < len(longPaths)-1; i++ {
		for j := i + 1; j < len(longPaths); j++ {
			if longPaths[j].rawRTT < longPaths[i].rawRTT {
				longPaths[i], longPaths[j] = longPaths[j], longPaths[i]
			}
		}
	}

	// Select paths: prioritize short paths, then long paths
	var result []Path
	remaining := count

	// Add short paths first
	for i := 0; i < len(shortPaths) && remaining > 0; i++ {
		result = append(result, shortPaths[i])
		remaining--
	}

	// Add long paths if needed
	for i := 0; i < len(longPaths) && remaining > 0; i++ {
		result = append(result, longPaths[i])
		remaining--
	}

	logger.Info("Path selection", slog.Int("total", len(paths)),
		slog.Int("short_paths", len(shortPaths)),
		slog.Int("long_paths", len(longPaths)),
		slog.Int("selected", count), slog.Any("selected_paths", result))

	return result
}

func (ks *KShortestSolver) calculatePathCost(path []string, graph_ map[string][]*graph.Edge) float64 {
	cost := 0.0
	for i := 0; i < len(path)-1; i++ {
		source := path[i]
		dest := path[i+1]
		for _, edge := range graph_[source] {
			if edge.DestinationIp == dest {
				if ks.useLatency {
					cost += edge.Latency * ks.alpha
				} else {
					cost += edge.EdgeWeight * ks.alpha
				}
				break
			}
		}
	}
	return cost
}

func (ks *KShortestSolver) calculatePathRTT(path []string, graph_ map[string][]*graph.Edge) float64 {
	rtt := 0.0
	for i := 0; i < len(path)-1; i++ {
		source := path[i]
		dest := path[i+1]
		for _, edge := range graph_[source] {
			if edge.DestinationIp == dest {
				rtt += edge.Latency
				break
			}
		}
	}
	return rtt
}
