package k_shortest

// shortest paths through Dijkstra
func Dijkstra(net Network, source int) [][]Path {
	n := len(net.Nodes)
	var results [][]Path = make([][]Path, n)

	newPath := Path{
		Nodes:   []int{source},
		Latency: 0,
	}
	results[source] = []Path{newPath}

	var latencies []int = make([]int, n) // latencies from source to every node
	for i := 0; i < n; i++ {
		latencies[i] = net.Links[source][i]
	}

	var visited []bool = make([]bool, n) // already have shortest paths
	visited[source] = true

	var predecessors [][]int = make([][]int, n) // predecessors of all nodes on their shortest paths
	for i := 0; i < n; i++ {
		predecessors[i] = []int{source}
	}
	predecessors[source] = []int{-1}

	for count := 0; count < n-1; count++ { // there are n-1 nodes except source
		minNode := -1
		for i := 0; i < n; i++ { // find the node with minimum latency
			if visited[i] || latencies[i] < 0 { // lagency < 0 means unreachable
				continue
			}
			if minNode < 0 || latencies[i] < latencies[minNode] {
				minNode = i
			}
		}
		if minNode == -1 { // all of rest nodes are unreachable
			break
		}
		// shortest paths to miniNode is finded
		visited[minNode] = true
		paths := findPaths(minNode, source, predecessors, latencies[minNode])
		results[minNode] = paths

		// update predecessors and latencies based on miniNode
		for i := 0; i < n; i++ {
			if visited[i] || net.Links[minNode][i] < 0 {
				continue
			}
			if latencies[i] < 0 || latencies[i] > latencies[minNode]+net.Links[minNode][i] {
				latencies[i] = latencies[minNode] + net.Links[minNode][i]
				predecessors[i] = []int{minNode}
			} else if latencies[i] == latencies[minNode]+net.Links[minNode][i] {
				predecessors[i] = append(predecessors[i], minNode)
			}
		}
	}

	return results
}

func findPaths(node int, source int, predecessors [][]int, latency int) []Path {
	var paths []Path

	var stack []int
	stack = append(stack, node)

	var find func()
	find = func() {
		if stack[len(stack)-1] == source { // a routing is finded
			nodes := []int{}
			for i := len(stack) - 1; i >= 0; i-- {
				nodes = append(nodes, stack[i])
			}
			newPath := Path{
				Nodes:   nodes,
				Latency: latency,
			}
			paths = append(paths, newPath)
			return
		}
		// find the predecessors of stack.top
		for _, predecessor := range predecessors[stack[len(stack)-1]] {
			stack = append(stack, predecessor)
			find()
			stack = stack[:len(stack)-1]
		}
	}
	find()

	return paths
}

// k_shortest paths through Yen's Algorithm
func KShortest(net Network, flow Flow, k int, hopThreshold int, theta int) []Path {
	var A []Path
	var B pathHeap
	if k == 0 {
		return A
	}
	shortest := Dijkstra(net, flow.Source)[flow.Destination]
	if len(shortest) == 0 { // unreachable
		return A
	}
	A = append(A, minPath(shortest))
	for len(A) < k {
		prevPath := A[len(A)-1].Nodes
		// The spur node ranges from the first node to the next to last node in the previous routing.
		for i := 0; i < len(prevPath)-1; i++ {
			spurNode := prevPath[i]
			rootPath := prevPath[:i+1]
			deletedLinks := make(map[[2]int]int) // map: tail, head -> latency.
			// Remove the links that are part of the previous shortest paths which share the same root routing.
			for j := 0; j < len(A); j++ {
				if len(A[j].Nodes) > i && sliceEqual(A[j].Nodes[:i+1], rootPath) {
					if _, exist := deletedLinks[[2]int{A[j].Nodes[i], A[j].Nodes[i+1]}]; !exist {
						deletedLinks[[2]int{A[j].Nodes[i], A[j].Nodes[i+1]}] = net.Links[A[j].Nodes[i]][A[j].Nodes[i+1]]
						net.Links[A[j].Nodes[i]][A[j].Nodes[i+1]] = -1
					}
				}
			}
			// Remove the nodes in rootPath except spurNode, make them unreachable.
			for j := 0; j < len(rootPath)-1; j++ {
				for head := 0; head < len(net.Nodes); head++ {
					if _, exist := deletedLinks[[2]int{head, rootPath[j]}]; !exist {
						deletedLinks[[2]int{head, rootPath[j]}] = net.Links[head][rootPath[j]]
						net.Links[head][rootPath[j]] = -1
					}
				}
			}
			// Calculate the spur routing from the spur node to the sink.
			spurPaths := Dijkstra(net, spurNode)[flow.Destination]
			// Add back the edges and nodes that were removed from the graph.
			for headTail, latency := range deletedLinks {
				net.Links[headTail[0]][headTail[1]] = latency
			}
			if len(spurPaths) > 0 { // spur routing is found
				// put together rootPath and spurPath to build totalPath
				spurPath := minPath(spurPaths).Nodes
				var rootPathCopy []int = make([]int, len(rootPath))
				copy(rootPathCopy, rootPath)
				totalPath := append(rootPathCopy[:len(rootPathCopy)-1], spurPath...)
				var latency int
				for j := 0; j < len(totalPath)-1; j++ {
					latency += net.Links[totalPath[j]][totalPath[j+1]]
				}

				hopCount := len(totalPath) - 1
				if hopCount > hopThreshold {

					//latency = int(math.Pow(float64(latency), float64(hopCount-hopThreshold)) * theta)
					latency = latency + (hopCount-hopThreshold)*theta
				}

				totalPathWithLatency := Path{
					Nodes:   totalPath,
					Latency: latency,
				}
				// insert totalPath to B
				if !B.contain(totalPathWithLatency) {
					B.insert(totalPathWithLatency)
				}
			}
		}
		// no more paths
		if len(B) == 0 {
			break
		}
		// let lowest cost routing in B become the k_shortest routing.
		// minHeap can guarantee that B[0] has the lowest cost
		A = append(A, B[0])
		// Remove the lowest cost routing in B
		B.pop()
	}
	return A
}

// whether two slices are equal
func sliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// minHeap for Path
type pathHeap []Path

// adjust minHeap from up to down
func (h pathHeap) shiftDown(start, end int) {
	var dad int = start
	var son int = dad*2 + 1

	for son <= end { // only compare when son is in range
		if son+1 <= end && pathLess(h[son+1], h[son]) { // choose the bigger son
			son++
		}
		if !pathLess(h[son], h[dad]) {
			break // adjustment is finished
		}
		h[dad], h[son] = h[son], h[dad]
		dad = son
		son = dad*2 + 1
	}
	return
}

// adjust minHeap from down to up
func (h pathHeap) shiftUp(start int) {
	var son int = start
	var dad int = (son - 1) / 2
	for dad >= 0 {
		if !pathLess(h[son], h[dad]) {
			break // // adjustment is finished
		}
		h[dad], h[son] = h[son], h[dad]
		son = dad
		dad = (son - 1) / 2
	}
	return
}

// build a minHeap from a Path slice
func buildHeap(h []Path) {
	// init heap from the last non-leaf node
	for i := len(h)/2 - 1; i >= 0; i-- {
		pathHeap(h).shiftDown(i, len(h)-1)
	}
	return
}

// insert p to h following the rules of minHeap
func (h *pathHeap) insert(p Path) {
	*h = append(*h, p)     // insert
	h.shiftUp(len(*h) - 1) // adjust
	return
}

// remove the minimum element
func (h *pathHeap) pop() {
	(*h)[0] = (*h)[len(*h)-1] // move the last element to the first
	*h = (*h)[:len(*h)-1]     // remove the last element
	h.shiftDown(0, len(*h)-1) // adjust
	return
}

// whether h contains p
func (h pathHeap) contain(p Path) bool {
	for i := 0; i < len(h); i++ {
		if sliceEqual(h[i].Nodes, p.Nodes) {
			return true
		}
	}
	return false
}

// whether p1 < p2
func pathLess(p1, p2 Path) bool {
	if p1.Latency < p2.Latency {
		return true
	}
	if p1.Latency > p2.Latency {
		return false
	}
	return len(p1.Nodes) < len(p2.Nodes)
}

// return the shortest routing in a Path slice
func minPath(paths []Path) Path {
	var min Path
	if len(paths) == 0 {
		return min
	}
	min = paths[0]
	for i := 1; i < len(paths); i++ {
		if pathLess(paths[i], min) {
			min = paths[i]
		}
	}
	return min
}
