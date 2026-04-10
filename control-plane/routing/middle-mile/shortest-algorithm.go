package middle_mile

import (
	"container/heap"
	"control-plane/routing/graph"
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

// Dijkstra
func (d *DijkstraSolver) Computing(start, end string, pre string, logger *slog.Logger) ([]string, float64) {

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
		return nil, math.Inf(1)
	}
	if _, ok := nodes[end]; !ok {
		return nil, math.Inf(1)
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
		// 弹出当前成本最低的节点
		u := heap.Pop(pq).(*PQNode)
		currNode := u.node
		currCost := u.cost

		// 【核心修复】添加校验：如果当前弹出的成本大于已记录的最短距离，直接跳过该节点（已处理过更优路径）
		if currCost > dist[currNode] {
			continue
		}

		// 到达终点，回溯路径并返回
		if currNode == end {
			// 通过prev映射回溯路径
			path := []string{}
			for node := end; node != ""; node = prev[node] {
				path = append([]string{node}, path...)
			}
			return path, currCost
		}

		// 遍历当前节点的邻接边，更新最短路径
		for _, e := range graph_[currNode] {
			nextNode := e.DestinationIp
			// 计算新路径成本
			newCost := currCost + e.EdgeWeight*d.alpha

			// 如果新路径更优，更新距离并推入优先级队列
			if newCost < dist[nextNode] {
				dist[nextNode] = newCost
				prev[nextNode] = currNode // 记录前驱节点
				heap.Push(pq, &PQNode{
					node: nextNode,
					cost: newCost,
				})
			}
		}
	}

	// 无法到达终点
	return nil, math.Inf(1)
}
