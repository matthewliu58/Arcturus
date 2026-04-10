package middle_mile

import (
	"container/heap"
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
	"math"
)

// Priority Queue
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
	edges []*graph.Edge // 只读图
	alpha float64
}

// 创建实例
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

// Computing 执行最短路径计算
func (d *DijkstraSolver) Computing(start, end, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {

	// 构建图和节点集合
	graph_ := make(map[string][]*graph.Edge)
	nodes := make(map[string]struct{})
	for _, e := range d.edges {
		graph_[e.SourceIp] = append(graph_[e.SourceIp], e)
		nodes[e.SourceIp] = struct{}{}
		nodes[e.DestinationIp] = struct{}{}
	}

	// 校验起点和终点是否存在
	if _, ok := nodes[start]; !ok {
		return nil, fmt.Errorf("start node %s not found", start)
	}
	if _, ok := nodes[end]; !ok {
		return nil, fmt.Errorf("end node %s not found", end)
	}

	// 初始化距离映射和前驱节点映射
	dist := make(map[string]float64)
	prev := make(map[string]string)
	for node := range nodes {
		dist[node] = math.Inf(1)
	}
	dist[start] = 0

	// 初始化优先级队列
	pq := &PriorityQueue{}
	heap.Init(pq)
	heap.Push(pq, &PQNode{
		node: start,
		cost: 0,
	})

	// 处理优先级队列
	for pq.Len() > 0 {
		u := heap.Pop(pq).(*PQNode)
		currNode := u.node
		currCost := u.cost

		if currCost > dist[currNode] {
			continue
		}

		// 到达终点，回溯路径并返回
		if currNode == end {
			path := []string{}
			for node := end; node != ""; node = prev[node] {
				path = append([]string{node}, path...)
			}
			logger.Info("Dijkstra path found", slog.String("pre", pre),
				slog.String("start", start), slog.String("end", end),
				slog.Any("path", path), slog.Float64("rtt", currCost))
			return []routing.PathInfo{{Hops: path, Rtt: currCost}}, nil
		}

		// 遍历当前节点的邻接边，更新最短路径
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

	// 无法到达终点
	return nil, fmt.Errorf("no path found from %s to %s", start, end)
}
