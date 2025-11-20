package graph

import (
	"math"
)

// Graph represents a directed graph with capacity and latency constraints
type Graph struct {
	Nodes     int                  // Number of nodes
	Edges     map[int]map[int]Edge // Adjacency list representation
	Source    int                  // Source node
	Sink      int                  // Sink node
	EdgeUsage map[int]map[int]int  // Tracks how many times each edge has been used
}

// Edge represents a directed edge with capacity and latency
type Edge struct {
	Capacity float64 // Maximum flow capacity
	Latency  float64 // Latency of the edge
	Flow     float64 // Current flow on the edge
}

// NewGraph creates a new graph with n nodes, source s, and sink t
func NewGraph(n, s, t int) *Graph {
	edges := make(map[int]map[int]Edge)
	edgeUsage := make(map[int]map[int]int)

	for i := 0; i < n; i++ {
		edges[i] = make(map[int]Edge)
		edgeUsage[i] = make(map[int]int)
	}

	return &Graph{
		Nodes:     n,
		Edges:     edges,
		Source:    s,
		Sink:      t,
		EdgeUsage: edgeUsage,
	}
}

// AddEdge adds a directed edge from u to v with given capacity and latency
func (g *Graph) AddEdge(u, v int, capacity, latency float64) {
	g.Edges[u][v] = Edge{
		Capacity: capacity,
		Latency:  latency,
		Flow:     0,
	}
	g.EdgeUsage[u][v] = 0
}

// Copy creates a deep copy of the graph
func (g *Graph) Copy() *Graph {
	newGraph := NewGraph(g.Nodes, g.Source, g.Sink)

	// Copy edges
	for u := range g.Edges {
		for v, edge := range g.Edges[u] {
			newGraph.Edges[u][v] = Edge{
				Capacity: edge.Capacity,
				Latency:  edge.Latency,
				Flow:     edge.Flow,
			}
		}
	}

	// Copy edge usage
	for u := range g.EdgeUsage {
		for v, usage := range g.EdgeUsage[u] {
			newGraph.EdgeUsage[u][v] = usage
		}
	}

	return newGraph
}

// CreateResidualGraph creates a residual graph based on current flow
func (g *Graph) CreateResidualGraph() *Graph {
	residual := NewGraph(g.Nodes, g.Source, g.Sink)

	// Copy edge usage
	for u := range g.EdgeUsage {
		for v, usage := range g.EdgeUsage[u] {
			residual.EdgeUsage[u][v] = usage
		}
	}

	for u := range g.Edges {
		for v, edge := range g.Edges[u] {
			// Forward edge with remaining capacity
			if edge.Capacity > edge.Flow {
				residual.AddEdge(u, v, edge.Capacity-edge.Flow, edge.Latency)
				residual.EdgeUsage[u][v] = g.EdgeUsage[u][v]
			}

			// Backward edge with flow that can be cancelled
			if edge.Flow > 0 {
				residual.AddEdge(v, u, edge.Flow, -edge.Latency)
				//
				residual.EdgeUsage[v][u] = 0
			}
		}
	}

	return residual
}

// Path represents a directed routing in the graph
type Path struct {
	Nodes   []int   // Sequence of nodes in the routing
	Flow    float64 // Bottleneck flow of the routing
	Latency float64 // Total latency of the routing
}

// Copy creates a deep copy of a routing
func (p *Path) Copy() *Path {
	nodesCopy := make([]int, len(p.Nodes))
	copy(nodesCopy, p.Nodes)
	return &Path{
		Nodes:   nodesCopy,
		Flow:    p.Flow,
		Latency: p.Latency,
	}
}

// UpdateEdgeUsage increases the usage count for all edges in the routing
func (g *Graph) UpdateEdgeUsage(path *Path) {
	for i := 0; i < len(path.Nodes)-1; i++ {
		u := path.Nodes[i]
		v := path.Nodes[i+1]
		g.EdgeUsage[u][v]++
	}
}

// GetPathLatency calculates the total latency of a routing
func GetPathLatency(g *Graph, path []int) float64 {
	totalLatency := 0.0
	for i := 0; i < len(path)-1; i++ {
		u := path[i]
		v := path[i+1]
		if edge, exists := g.Edges[u][v]; exists {
			totalLatency += edge.Latency
		}
	}
	return totalLatency
}

// GetMaxFlow calculates the maximum possible flow through a routing
func GetMaxFlow(g *Graph, path []int) float64 {
	if len(path) < 2 {
		return 0
	}

	maxFlow := math.Inf(1)
	for i := 0; i < len(path)-1; i++ {
		u := path[i]
		v := path[i+1]
		if edge, exists := g.Edges[u][v]; exists {
			if edge.Capacity-edge.Flow < maxFlow {
				maxFlow = edge.Capacity - edge.Flow
			}
		}
	}

	return maxFlow
}
