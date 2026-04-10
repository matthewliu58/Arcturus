package routing

import (
	"control-plane/routing/graph"
	middle_mile "control-plane/routing/middle-mile"
	"log/slog"
)

type Path struct {
	Path []string
	Cost float64
}

type PathInfo struct {
	Hops []string `json:"hops"`
	Rtt  float64  `json:"rtt"`
	//Weight int64  `json:"weight"`
}

type RoutingInfo struct {
	Routing []PathInfo `json:"routing"`
}

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
	Shortest = "shortest"
)

type ComputingInterface interface {
	Computing(start, end, pre string, logger *slog.Logger) ([]string, float64)
}

type RoutingInterface struct {
	Operate ComputingInterface
}

func InitInterface(g *graph.GraphManager, algorithm string, pre string, logger *slog.Logger) RoutingInterface {
	switch algorithm {
	case Shortest:
		edges := g.GetEdges()
		solver := middle_mile.NewDijkstraSolver(edges)
		return RoutingInterface{Operate: solver}
	default:
		return RoutingInterface{}
	}
}

// 输入是client区域和cloud storage 区域
func MiddleRouting(g *graph.GraphManager, endPoints EndPoints, algorithm, pre string, logger *slog.Logger) RoutingInfo {

	logger.Info("Routing", slog.String("pre", pre), slog.Any("endPoints", endPoints))

	solver := InitInterface(g, algorithm, pre, logger)
	path, cost := solver.Operate.Computing(endPoints.Source.IP, endPoints.Dest.IP, pre, logger)
	if len(path) == 0 {
		logger.Warn("No cloud node found", slog.String("pre", pre))
		return RoutingInfo{}
	}
	logger.Info("All candidate paths", slog.String("pre", pre), slog.Any("path", path), slog.Float64("cost", cost))
	
	var paths []PathInfo
	paths = append(paths, PathInfo{path, cost})
	rout := RoutingInfo{Routing: paths}

	logger.Info("routing result", slog.String("pre", pre), slog.Any("rout", rout))
	return rout
}
