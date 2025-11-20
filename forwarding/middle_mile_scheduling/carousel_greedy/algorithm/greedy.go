package algorithm

import (
	"forwarding/middle_mile_scheduling/carousel_greedy/graph"

	log "github.com/sirupsen/logrus"
)

func GreedyMFPC(g *graph.Graph, thetaA, thetaL float64, maxEdgeUsage int) []*graph.Path {

	paths := []*graph.Path{}
	workingGraph := g.Copy()

	pathCount := 0
	for {
		pathCount++

		pathFinder := NewPathFinder(workingGraph, thetaA, thetaL, maxEdgeUsage)
		path := pathFinder.FindPath()

		if path == nil {
			break
		}

		paths = append(paths, path)

		UpdateResidualGraph(workingGraph, path)
	}

	totalFlow := CalculateTotalFlow(paths)
	log.Infof("GreedyMFPC completed: found %d paths with total flow %.2f", len(paths), totalFlow)

	return paths
}

func CalculateTotalFlow(paths []*graph.Path) float64 {
	totalFlow := 0.0
	for _, path := range paths {
		totalFlow += path.Flow
	}
	return totalFlow
}
