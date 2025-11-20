package algorithm

import (
	"fmt"
	"forwarding/middle_mile_scheduling/carousel_greedy/graph"
	"strings"
)

type PathFinder struct {
	g            *graph.Graph
	thetaA       float64
	thetaL       float64
	maxEdgeUsage int
	visited      map[int]bool
	currentPath  []int
	latencySoFar float64
	bannedArcs   map[string]bool
}

func NewPathFinder(g *graph.Graph, thetaA, thetaL float64, maxEdgeUsage int) *PathFinder {

	return &PathFinder{
		g:            g,
		thetaA:       thetaA,
		thetaL:       thetaL,
		maxEdgeUsage: maxEdgeUsage,
		visited:      make(map[int]bool),
		currentPath:  []int{},
		latencySoFar: 0,
		bannedArcs:   make(map[string]bool),
	}
}

func (pf *PathFinder) banArc(from, to int) {
	key := fmt.Sprintf("%d->%d", from, to)
	pf.bannedArcs[key] = true
}

func (pf *PathFinder) banArcs(arcsInfo []struct {
	From   int
	To     int
	Reason string
}) {
	if len(arcsInfo) == 0 {
		return
	}

	var bannedArcsInfo strings.Builder

	for i, arcInfo := range arcsInfo {
		if i > 0 {
			bannedArcsInfo.WriteString(", ")
		}

		key := fmt.Sprintf("%d->%d", arcInfo.From, arcInfo.To)
		pf.bannedArcs[key] = true

		bannedArcsInfo.WriteString(fmt.Sprintf("%d->%d", arcInfo.From, arcInfo.To))
	}

}

func (pf *PathFinder) unbanArc(from, to int) {
	key := fmt.Sprintf("%d->%d", from, to)
	delete(pf.bannedArcs, key)
}

func (pf *PathFinder) unbanArcs(arcs []struct{ From, To int }) {
	if len(arcs) == 0 {
		return
	}

	var unbannedArcsInfo strings.Builder

	for i, arc := range arcs {
		if i > 0 {
			unbannedArcsInfo.WriteString(", ")
		}

		key := fmt.Sprintf("%d->%d", arc.From, arc.To)
		delete(pf.bannedArcs, key)

		unbannedArcsInfo.WriteString(fmt.Sprintf("%d->%d", arc.From, arc.To))
	}

}

func (pf *PathFinder) isArcBanned(from, to int) bool {
	key := fmt.Sprintf("%d->%d", from, to)
	return pf.bannedArcs[key]
}

func (pf *PathFinder) FindPath() *graph.Path {

	pf.visited = make(map[int]bool)
	pf.currentPath = nil
	pf.latencySoFar = 0

	found := pf.findPathDFS(pf.g.Source)

	if !found || len(pf.currentPath) < 2 {
		return nil //
	}

	flow := graph.GetMaxFlow(pf.g, pf.currentPath)
	latency := graph.GetPathLatency(pf.g, pf.currentPath)

	return &graph.Path{
		Nodes:   append([]int{}, pf.currentPath...),
		Flow:    flow,
		Latency: latency,
	}
}

func (pf *PathFinder) findPathDFS(node int) bool {

	pf.visited[node] = true
	pf.currentPath = append(pf.currentPath, node)

	if node == pf.g.Sink {
		return true
	}

	neighbors := pf.getNeighborsSortedByCapacity(node)

	for _, nextNode := range neighbors {

		if pf.visited[nextNode] {
			continue
		}

		edge := pf.g.Edges[node][nextNode]

		if pf.g.EdgeUsage[node][nextNode] >= pf.maxEdgeUsage {
			continue
		}

		if pf.isArcBanned(node, nextNode) {
			continue
		}

		if edge.Flow >= edge.Capacity*pf.thetaA {
			continue
		}

		newLatency := pf.latencySoFar + edge.Latency
		if newLatency > pf.thetaL {
			continue
		}

		oldLatency := pf.latencySoFar
		pf.latencySoFar = newLatency

		if pf.findPathDFS(nextNode) {
			return true
		}

		pf.latencySoFar = oldLatency
	}

	pf.currentPath = pf.currentPath[:len(pf.currentPath)-1]
	pf.visited[node] = false

	return false
}

func (pf *PathFinder) getNeighborsSortedByCapacity(node int) []int {
	type nodeCapacity struct {
		node     int
		capacity float64
	}

	var neighbors []nodeCapacity

	for neighbor, edge := range pf.g.Edges[node] {
		availableCapacity := edge.Capacity - edge.Flow
		if availableCapacity > 0 {
			neighbors = append(neighbors, nodeCapacity{
				node:     neighbor,
				capacity: availableCapacity,
			})
		}
	}

	for i := 0; i < len(neighbors); i++ {
		for j := i + 1; j < len(neighbors); j++ {
			if neighbors[j].capacity > neighbors[i].capacity {
				neighbors[i], neighbors[j] = neighbors[j], neighbors[i]
			}
		}
	}

	result := make([]int, len(neighbors))
	for i, nc := range neighbors {
		result[i] = nc.node
	}

	return result
}
