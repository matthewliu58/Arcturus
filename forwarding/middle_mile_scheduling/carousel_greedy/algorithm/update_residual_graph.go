package algorithm

import (
	"forwarding/middle_mile_scheduling/carousel_greedy/graph"
)

func UpdateResidualGraph(g *graph.Graph, path *graph.Path) {
	if path == nil || len(path.Nodes) < 2 {
		return
	}

	for i := 0; i < len(path.Nodes)-1; i++ {
		u := path.Nodes[i]
		v := path.Nodes[i+1]

		edge := g.Edges[u][v]
		edge.Flow += path.Flow
		g.Edges[u][v] = edge
	}

	g.UpdateEdgeUsage(path)

}
