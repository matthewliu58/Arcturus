package routing

import (
	agg "control-plane/info-agg"
	"control-plane/routing/graph"
	last "control-plane/routing/last-mile"
	middle "control-plane/routing/middle-mile"
	"control-plane/routing/routing"
	"log/slog"
)

const (
	Shortest       = "shortest"
	CarouselGreedy = "carousel_greed"
	Lyapunov       = "lyapunov"
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
		solver := middle.NewDijkstraSolver(edges)
		return RoutingMiddleInterface{Operate: solver}
	case CarouselGreedy:
		edges := g.GetEdges()
		solver := middle.NewHeuristicSolver(edges)
		return RoutingMiddleInterface{Operate: solver}
	default:
		return RoutingMiddleInterface{}
	}
}

func MiddleRouting(g *graph.GraphManager, endPoints routing.EndPoints, algorithm, pre string, logger *slog.Logger) routing.RoutingInfo {

	logger.Info("MiddleRouting", slog.String("pre", pre), slog.Any("endPoints", endPoints))

	solver := InitMiddleInterface(g, algorithm, pre, logger)
	paths, err := solver.Operate.Computing(endPoints.Source.IP, endPoints.Dest.IP, pre, logger)
	if err != nil {
		logger.Warn("No routing", slog.String("pre", pre), slog.Any("err", err))
		return routing.RoutingInfo{}
	}
	logger.Info("MiddleRouting", slog.String("pre", pre), slog.Any("paths", paths))

	rout := routing.RoutingInfo{Routing: paths}
	logger.Info("MiddleRouting result", slog.String("pre", pre), slog.Any("rout", rout))

	return rout
}

type ComputingLastInterface interface {
	Computing(endPoints routing.EndPoints, pre string, logger *slog.Logger) ([]routing.PathInfo, error)
}

type RoutingLastInterface struct {
	Operate ComputingLastInterface
}

func InitLastInterface(g *graph.GraphManager, a *agg.GlobalStats,
	algorithm string, pre string, logger *slog.Logger) RoutingLastInterface {
	switch algorithm {
	case Lyapunov:
		nodes := g.GetNodes()
		edgeAgg := a.GetAggMap()
		nodeLocation := a.GetNodeLocation()
		solver := last.NewLyapunovSolver(edgeAgg, nodes, nodeLocation)
		return RoutingLastInterface{Operate: solver}
	default:
		return RoutingLastInterface{}
	}
}

func LastRouting(g *graph.GraphManager, a *agg.GlobalStats, endPoints routing.EndPoints, algorithm,
	pre string, logger *slog.Logger) routing.RoutingInfo {

	logger.Info("LastRouting", slog.String("pre", pre), slog.Any("endPoints", endPoints))

	solver := InitLastInterface(g, a, algorithm, pre, logger)
	paths, err := solver.Operate.Computing(endPoints, pre, logger)
	if err != nil {
		logger.Warn("No routing", slog.String("pre", pre), slog.Any("err", err))
		return routing.RoutingInfo{}
	}
	logger.Info("LastRouting", slog.String("pre", pre), slog.Any("paths", paths))

	rout := routing.RoutingInfo{Routing: paths}
	logger.Info("LastRouting result", slog.String("pre", pre), slog.Any("rout", rout))

	return rout
}
