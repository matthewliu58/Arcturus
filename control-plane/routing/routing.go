package routing

import (
	agg "control-plane/aggregator"
	middle "control-plane/routing/core-domain"
	last "control-plane/routing/edge-domain"
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"control-plane/util"
	"log/slog"
)

const ( //middle
	Shortest       = "shortest"
	KShortest      = "k_shortest"
	CarouselGreedy = "carousel_greed"
	LiveNet        = "live_net"
	LiveStyle      = "live_style"
	ONEWAN         = "onewan"
	ONEWANMulti    = "onewan_multi" // 1-source → N-destinations
)
const ( //last
	Lyapunov        = "lyapunov"
	LatencyOnly     = "latency_only"
	CPUOnly         = "cpu_only"
	JointCpuLatency = "joint_cpu_latency"
	P2C             = "p2c"
	EWMABased       = "ewma_based"
)

type ComputingMiddleInterface interface {
	Computing(start, end, pre string, logger *slog.Logger) ([]routing.PathInfo, error)
}

type RoutingMiddleInterface struct {
	Operate ComputingMiddleInterface
}

func InitMiddleInterface(g *graph.GraphManager, algorithm string, pre string, logger *slog.Logger) RoutingMiddleInterface {
	edges := g.GetEdges()
	switch algorithm {
	case Shortest:
		solver := middle.NewDijkstraSolver(edges)
		return RoutingMiddleInterface{Operate: solver}
	case CarouselGreedy:
		solver := middle.NewHeuristicSolver(edges)
		return RoutingMiddleInterface{Operate: solver}
	case KShortest:
		solver := middle.NewKShortestSolver(edges, 3) // 默认 k=3
		return RoutingMiddleInterface{Operate: solver}
	case LiveNet:
		solver := middle.NewLiveNetSolver(edges, 3) // 默认 k=3
		return RoutingMiddleInterface{Operate: solver}
	case LiveStyle:
		solver := middle.NewLiveStyleSolver(edges, 2) // 默认 k=2
		return RoutingMiddleInterface{Operate: solver}
	case ONEWAN:
		solver := middle.NewONEWANSolver(edges, 10) // 默认 maxPaths=10
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

func InitLastInterface(g *graph.GraphManager, a *agg.GlobalStats, algorithm string,
	cpuWeight, latencyWeight float64, pre string, logger *slog.Logger) RoutingLastInterface {
	nodes := g.GetNodes()
	edgeAgg := a.GetAggMap()
	switch algorithm {
	case Lyapunov:
		solver := last.NewLyapunovSolver(edgeAgg, nodes)
		return RoutingLastInterface{Operate: solver}
	case LatencyOnly:
		// Fallback to JointRouter with CPU weight = 0
		router := last.NewJointRouter(edgeAgg, nodes)
		router.SetWeights(0, 1)
		return RoutingLastInterface{Operate: router}
	case CPUOnly:
		// Fallback to JointRouter with latency weight = 0
		router := last.NewJointRouter(edgeAgg, nodes)
		router.SetWeights(1, 0)
		return RoutingLastInterface{Operate: router}
	case JointCpuLatency:
		router := last.NewJointRouter(edgeAgg, nodes)
		// Set weights if provided
		if cpuWeight >= 0 && latencyWeight >= 0 {
			router.SetWeights(cpuWeight, latencyWeight)
		}
		return RoutingLastInterface{Operate: router}
	case P2C:
		// Power-of-Two Choices: proportional distribution based on inverse CPU load
		router := last.NewP2CRouter(nodes, edgeAgg)
		return RoutingLastInterface{Operate: router}
	case EWMABased:
		// EWMA-based: use exponentially weighted moving average for smoothed control
		router := last.NewEWMARouter(nodes, edgeAgg)
		// Set alpha and lambda if provided
		if cpuWeight >= 0 && latencyWeight >= 0 {
			router.SetAlpha(cpuWeight, latencyWeight)
		}
		return RoutingLastInterface{Operate: router}
	default:
		return RoutingLastInterface{}
	}
}

func LastRouting(g *graph.GraphManager, a *agg.GlobalStats, endPoints routing.EndPoints, algorithm string,
	cpuWeight, latencyWeight float64, pre string, logger *slog.Logger) routing.RoutingInfo {

	logger.Info("LastRouting", slog.String("pre", pre), slog.Any("endPoints", endPoints),
		slog.Float64("cpuWeight", cpuWeight), slog.Float64("latencyWeight", latencyWeight))

	//if len(endPoints.Source.City) <= 0 {
	result, err := util.GetIPInfo(endPoints.Source.IP)
	if err != nil {
		logger.Error("GetIPInfo error", slog.String("pre", pre), slog.Any("err", err))
		return routing.RoutingInfo{}
	}
	endPoints.Source.Country = result.Country
	endPoints.Source.City = result.City
	endPoints.Source.Continent = result.Continent
	//}

	solver := InitLastInterface(g, a, algorithm, cpuWeight, latencyWeight, pre, logger)
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

// --- Multi-endpoint routing (1 source → N destinations) ---

// ComputingMiddleMultiInterface supports routing from one source to multiple destinations.
type ComputingMiddleMultiInterface interface {
	ComputingMulti(start string, ends []string, pre string, logger *slog.Logger) ([]routing.PathInfo, error)
}

// RoutingMiddleMultiInterface wraps a multi-end middle-router solver.
type RoutingMiddleMultiInterface struct {
	Operate ComputingMiddleMultiInterface
}

// InitMiddleMultiInterface creates a solver for 1→N destination routing.
func InitMiddleMultiInterface(g *graph.GraphManager, algorithm string, pre string, logger *slog.Logger) RoutingMiddleMultiInterface {
	edges := g.GetEdges()
	switch algorithm {
	case ONEWANMulti:
		solver := middle.NewONEWANSolver(edges, 2) // 2 paths per destination
		return RoutingMiddleMultiInterface{Operate: solver}
	default:
		return RoutingMiddleMultiInterface{}
	}
}

// MiddleRoutingMulti computes routes from one source to multiple destinations.
// Each destination gets up to maxPaths diverse paths (default 2 for onewan_multi).
func MiddleRoutingMulti(g *graph.GraphManager, start string, ends []string, algorithm, pre string, logger *slog.Logger) routing.RoutingInfo {
	logger.Info("MiddleRoutingMulti", slog.String("pre", pre),
		slog.String("start", start), slog.Any("ends", ends))

	solver := InitMiddleMultiInterface(g, algorithm, pre, logger)
	if solver.Operate == nil {
		logger.Warn("MiddleRoutingMulti: unsupported algorithm",
			slog.String("pre", pre), slog.String("algorithm", algorithm))
		return routing.RoutingInfo{}
	}

	paths, err := solver.Operate.ComputingMulti(start, ends, pre, logger)
	if err != nil {
		logger.Warn("MiddleRoutingMulti failed", slog.String("pre", pre), slog.Any("err", err))
		return routing.RoutingInfo{}
	}

	rout := routing.RoutingInfo{Routing: paths}
	logger.Info("MiddleRoutingMulti result", slog.String("pre", pre),
		slog.Int("paths", len(paths)), slog.Any("rout", rout))

	return rout
}
