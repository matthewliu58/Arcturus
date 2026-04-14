package middle_mile

import (
	"container/heap"
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
	"math"
)

type PQNode struct {
	node  string
	cost  float64
	index int
}

type PriorityQueue []*PQNode

func (pq PriorityQueue) Len() int           { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool { return pq[i].cost < pq[j].cost }
func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index, pq[j].index = i, j
}

func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*PQNode)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

type DijkstraSolver struct {
	edges []*graph.Edge
	alpha float64
}

func NewDijkstraSolver(edges []*graph.Edge) *DijkstraSolver {
	var g []*graph.Edge
	for _, e := range edges {
		g = append(g, e)
	}
	return &DijkstraSolver{
		edges: g,
		alpha: 1.2,
	}
}

func (d *DijkstraSolver) Computing(start, end, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {

	graph_ := make(map[string][]*graph.Edge)
	nodes := make(map[string]struct{})
	for _, e := range d.edges {
		graph_[e.SourceIp] = append(graph_[e.SourceIp], e)
		nodes[e.SourceIp] = struct{}{}
		nodes[e.DestinationIp] = struct{}{}
	}

	if _, ok := nodes[start]; !ok {
		return nil, fmt.Errorf("start node %s not found", start)
	}
	if _, ok := nodes[end]; !ok {
		return nil, fmt.Errorf("end node %s not found", end)
	}

	dist := make(map[string]float64)
	prev := make(map[string]string)
	for node := range nodes {
		dist[node] = math.Inf(1)
	}
	dist[start] = 0

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
			var path []string
			for node := end; node != ""; node = prev[node] {
				path = append([]string{node}, path...)
			}
			logger.Info("Dijkstra path found", slog.String("pre", pre),
				slog.String("start", start), slog.String("end", end),
				slog.Any("path", path), slog.Float64("rtt", currCost))
			return []routing.PathInfo{{Hops: path, Rtt: currCost}}, nil
		}

		for _, e := range graph_[currNode] {
			nextNode := e.DestinationIp
			newCost := currCost + e.EdgeWeight*d.alpha

			if newCost < dist[nextNode] {
				dist[nextNode] = newCost
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
