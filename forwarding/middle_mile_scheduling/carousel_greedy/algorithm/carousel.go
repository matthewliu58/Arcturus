package algorithm

import (
	"fmt"
	"forwarding/middle_mile_scheduling/carousel_greedy/graph"
	"strings"

	log "github.com/sirupsen/logrus"
)

type PathFrequency struct {
	Path      *graph.Path
	Frequency int
	Age       int //
}

// CarouselGreedy MFPCCarousel Greedy
func CarouselGreedy(g *graph.Graph, thetaA, thetaL float64, maxEdgeUsage, alpha, beta int) []*graph.Path {
	log.Infof("Starting Carousel Greedy algorithm with thetaA=%.2f, thetaL=%.2f, maxEdgeUsage=%d, alpha=%d, beta=%d",
		thetaA, thetaL, maxEdgeUsage, alpha, beta)

	initialGraph := g.Copy()
	initialSolution := GreedyMFPC(initialGraph, thetaA, thetaL, maxEdgeUsage)

	if len(initialSolution) == 0 {
		log.Warn("No initial solution found")
		return initialSolution
	}

	log.Infof("Initial solution found with %d paths", len(initialSolution))

	pathFrequencies := make(map[string]*PathFrequency)
	for i, path := range initialSolution {
		key := pathToString(path)
		pathFrequencies[key] = &PathFrequency{
			Path:      path.Copy(),
			Frequency: 1,
			Age:       i,
		}
	}

	var currentSolution []*graph.Path
	for _, path := range initialSolution {
		currentSolution = append(currentSolution, path.Copy())
	}

	bestSolution := make([]*graph.Path, len(currentSolution))
	copy(bestSolution, currentSolution)
	bestObjective := CalculateTotalFlow(bestSolution)
	log.Infof("Initial best objective (total flow): %.2f", bestObjective)

	workingGraph := g.Copy()
	for _, path := range currentSolution {
		UpdateResidualGraph(workingGraph, path)
	}

	bannedArcs := make(map[string]bool)

	if beta > 0 && len(currentSolution) > beta {
		log.Infof("Removing %d oldest paths from solution", beta)

		removedPaths := currentSolution[len(currentSolution)-beta:]
		currentSolution = currentSolution[:len(currentSolution)-beta]

		for _, path := range removedPaths {
			// resetPathFlow to restore the flow on these paths
			resetPathFlow(workingGraph, path, bannedArcs)
		}
	}

	numIterations := alpha * len(initialSolution)
	if numIterations <= 0 {
		numIterations = 1
	}

	for k := 0; k < numIterations; k++ {

		if len(currentSolution) == 0 {

			break
		}

		oldestPath := currentSolution[0]
		currentSolution = currentSolution[1:]

		var oldestPathFirstArc struct {
			From int
			To   int
		}
		if len(oldestPath.Nodes) >= 2 {
			oldestPathFirstArc.From = oldestPath.Nodes[0]
			oldestPathFirstArc.To = oldestPath.Nodes[1]
		}

		resetPathFlow(workingGraph, oldestPath, bannedArcs)

		var maxFreqPath *graph.Path
		maxFreq := 0
		maxAge := -1

		for _, pf := range pathFrequencies {

			if pf.Frequency > maxFreq || (pf.Frequency == maxFreq && pf.Age < maxAge) {
				maxFreq = pf.Frequency
				maxAge = pf.Age
				maxFreqPath = pf.Path

			}
		}

		var arcsToBeDisable []struct {
			From   int
			To     int
			Reason string
		}

		if len(oldestPath.Nodes) >= 2 {
			arcsToBeDisable = append(arcsToBeDisable, struct {
				From   int
				To     int
				Reason string
			}{
				From:   oldestPathFirstArc.From,
				To:     oldestPathFirstArc.To,
				Reason: "",
			})

			key := fmt.Sprintf("%d->%d", oldestPathFirstArc.From, oldestPathFirstArc.To)
			bannedArcs[key] = true
		}

		if maxFreqPath != nil && len(maxFreqPath.Nodes) >= 2 {
			for i := 0; i < len(maxFreqPath.Nodes)-1; i++ {
				from := maxFreqPath.Nodes[i]
				to := maxFreqPath.Nodes[i+1]

				duplicate := false
				for _, arc := range arcsToBeDisable {
					if arc.From == from && arc.To == to {
						duplicate = true
						break
					}
				}

				if !duplicate {
					arcsToBeDisable = append(arcsToBeDisable, struct {
						From   int
						To     int
						Reason string
					}{
						From:   from,
						To:     to,
						Reason: "",
					})

					key := fmt.Sprintf("%d->%d", from, to)
					bannedArcs[key] = true
				}
			}
		}

		newPathCount := 0
		for {

			residual := workingGraph.CreateResidualGraph()
			pathFinder := NewPathFinder(residual, thetaA, thetaL, maxEdgeUsage)

			if len(arcsToBeDisable) > 0 {
				pathFinder.banArcs(arcsToBeDisable)
			}

			newPath := pathFinder.FindPath()
			if newPath == nil {
				if newPathCount == 0 {

				} else {

				}
				break
			}

			newPathCount++

			currentSolution = append(currentSolution, newPath)

			pathKey := pathToString(newPath)
			if pf, exists := pathFrequencies[pathKey]; exists {
				pf.Frequency++

			} else {
				newAge := len(pathFrequencies)
				pathFrequencies[pathKey] = &PathFrequency{
					Path:      newPath.Copy(),
					Frequency: 1,
					Age:       newAge,
				}

			}

			UpdateResidualGraph(workingGraph, newPath)
		}

		currentObjective := CalculateTotalFlow(currentSolution)

		if currentObjective > bestObjective {

			bestSolution = make([]*graph.Path, len(currentSolution))
			copy(bestSolution, currentSolution)
			bestObjective = currentObjective
		}
	}

	log.Infof("Carousel Greedy completed: found %d paths",
		len(bestSolution))

	return bestSolution
}

func resetPathFlow(g *graph.Graph, path *graph.Path, bannedArcs map[string]bool) {
	if path == nil || len(path.Nodes) < 2 {
		return
	}

	var arcsToUnban []struct{ From, To int }

	for i := 0; i < len(path.Nodes)-1; i++ {
		u := path.Nodes[i]
		v := path.Nodes[i+1]

		if edge, exists := g.Edges[u][v]; exists {
			edge.Flow -= path.Flow
			if edge.Flow <= 0 {

				edge.Flow = 0

				key := fmt.Sprintf("%d->%d", u, v)
				if _, banned := bannedArcs[key]; banned {

					delete(bannedArcs, key)
					arcsToUnban = append(arcsToUnban, struct{ From, To int }{u, v})
				}
			}
			g.Edges[u][v] = edge
		}

		if g.EdgeUsage[u][v] > 0 {
			g.EdgeUsage[u][v]--
		}
	}

	if len(arcsToUnban) > 0 {
		var unbannedInfo strings.Builder
		for i, arc := range arcsToUnban {
			if i > 0 {
				unbannedInfo.WriteString(", ")
			}
			unbannedInfo.WriteString(fmt.Sprintf("%d->%d", arc.From, arc.To))
		}

	}
}

func pathToString(p *graph.Path) string {
	if p == nil || len(p.Nodes) == 0 {
		return ""
	}

	var builder strings.Builder
	for i, node := range p.Nodes {
		if i > 0 {
			builder.WriteString("->")
		}
		builder.WriteString(fmt.Sprintf("%d", node))
	}
	return builder.String()
}
