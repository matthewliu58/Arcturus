package routing

import (
	"control-plane/routing/graph"
	middle_mile "control-plane/routing/middle-mile"
	"control-plane/routing/routing"
	"log/slog"
)

type EndPoint struct {
	IP        string `json:"ip"`
	Provider  string `json:"provider"`
	Continent string `json:"continent"`
	Country   string `json:"country"`
	City      string `json:"city"`
}

type EndPoints struct {
	Source EndPoint `json:"source"`
	Dest   EndPoint `json:"dest"`
}

const (
	Shortest       = "shortest"
	CarouselGreedy = "carousel_greed"
)

type ComputingMiddleInterface interface {
	Computing(start, end, pre string, logger *slog.Logger) ([]routing.PathInfo, error)
}

type RoutingMiddleInterface struct {
	Operate ComputingMiddleInterface
}

func InitMiddleInterface(g *graph.GraphManager, algorithm string, pre string, logger *slog.Logger) RoutingMiddleInterface {
	switch algorithm {
	case Shortest:
		edges := g.GetEdges()
		solver := middle_mile.NewDijkstraSolver(edges)
		return RoutingMiddleInterface{Operate: solver}
	case CarouselGreedy:
		edges := g.GetEdges()
		solver := middle_mile.NewHeuristicSolver(edges)
		return RoutingMiddleInterface{Operate: solver}
	default:
		return RoutingMiddleInterface{}
	}
}

func MiddleRouting(g *graph.GraphManager, endPoints EndPoints, algorithm, pre string, logger *slog.Logger) routing.RoutingInfo {

	logger.Info("Routing", slog.String("pre", pre), slog.Any("endPoints", endPoints))

	solver := InitMiddleInterface(g, algorithm, pre, logger)
	paths, err := solver.Operate.Computing(endPoints.Source.IP, endPoints.Dest.IP, pre, logger)
	if err != nil {
		logger.Warn("No routing", slog.String("pre", pre), slog.Any("err", err))
		return routing.RoutingInfo{}
	}
	logger.Info("MiddleRouting", slog.String("pre", pre), slog.Any("paths", paths))

	rout := routing.RoutingInfo{Routing: paths}
	logger.Info("routing result", slog.String("pre", pre), slog.Any("rout", rout))

	return rout
}
