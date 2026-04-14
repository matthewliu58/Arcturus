package graph

import (
	agg "control-plane/aggregator"
	"log/slog"
	"math"
	"strconv"
	"sync"
)

type Edge struct {
	mu            sync.RWMutex
	SourceIp      string  `json:"source_ip"`
	DestinationIp string  `json:"destination_ip"`
	EdgeWeight    float64 `json:"edge_weight"`
	Latency       float64 `json:"latency"`
	Loss          float64 `json:"loss"`
}

func (e *Edge) UpdateWeight(newWeight float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.EdgeWeight = newWeight
}

func (e *Edge) Weight() float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.EdgeWeight
}

type GraphManager struct {
	mu     sync.RWMutex
	edges  map[string]*Edge
	nodes  map[string]*agg.Telemetry
	logger *slog.Logger
}

func NewGraphManager(logger *slog.Logger) *GraphManager {
	return &GraphManager{
		edges:  make(map[string]*Edge),
		nodes:  make(map[string]*agg.Telemetry),
		logger: logger,
	}
}

func (g *GraphManager) GetNode(id string) (*agg.Telemetry, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	node, ok := g.nodes[id]
	return node, ok
}

func (g *GraphManager) GetNodes() map[string]*agg.Telemetry {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make(map[string]*agg.Telemetry)
	for k, node := range g.nodes {
		nodes[k] = node
	}

	return nodes
}

func (g *GraphManager) RemoveNode(id, logPre string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.nodes, id)

	for key, e := range g.edges {
		if e.SourceIp == id || e.DestinationIp == id {
			delete(g.edges, key)
		}
	}
}

func (g *GraphManager) GetEdges() []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	list := make([]*Edge, 0, len(g.edges))
	for _, e := range g.edges {
		list = append(list, e)
	}
	return list
}

func (g *GraphManager) GetEdge(edgeID string) *Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if e, ok := g.edges[edgeID]; ok {
		return e
	}
	return nil
}

func (g *GraphManager) AddNode(node *agg.Telemetry, logPre string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.nodes[node.PublicIP] = node

	for _, v := range node.LinksCongestion {

		var in, out, newLine string
		var r float64

		if v.Target.TargetType == "cloud_storage" {

			in = node.PublicIP
			out = v.Target.IP + ":" + strconv.Itoa(v.Target.Port)

			newLine = in + "->" + out
			r = EdgeRisk(0, v.PacketLoss, v.AverageLatency, logPre, g.logger)
			//g.logger.Info("EdgeRisk", slog.String("pre", logPre), out+"->"+cloudFull, r)
		} else {
			in = node.PublicIP
			out = v.Target.IP
			outNode, ok := g.nodes[out]
			if !ok {
				g.logger.Warn("out node not found", slog.String("pre", logPre), slog.String("out", out))
				continue
			}
			r = EdgeRisk(outNode.CpuPressure, v.PacketLoss, v.AverageLatency, logPre, g.logger)
		}

		oldLine, ok := g.edges[newLine]
		if ok {
			oldLine.EdgeWeight = r
		} else {
			g.edges[newLine] = &Edge{
				SourceIp:      in,
				DestinationIp: out,
				EdgeWeight:    r,
			}
		}
	}
}

func (g *GraphManager) DumpGraph(logPre string) {

	g.logger.Debug("DumpGraph", slog.String("pre", logPre))
	for _, node := range g.GetNodes() {
		g.logger.Debug("Graph Node", slog.String("pre", logPre), slog.Any("node", node))
	}
	for _, edge := range g.GetEdges() {
		g.logger.Debug("Graph Edge", slog.String("pre", logPre), slog.Any("edge", edge))
	}
}

func EdgeRisk(cpuPressure, loss, latency float64, pre string, l *slog.Logger) float64 {

	l.Info("EdgeRisk", slog.String("pre", pre),
		slog.Float64("cpuPressure", cpuPressure),
		slog.Float64("loss", loss),
		slog.Float64("latency", latency))

	const (
		cpuNormalLine = 40.0
		cpuWarnLine   = 70.0
		cpuMaxLine    = 100.0

		lossInflection = 0.03
		lossSharpness  = 50.0

		latencyWarn     = 80.0
		latencyCritical = 200.0
		latencyMax      = 500.0

		wCPU  = 0.4
		wLoss = 0.3
		wLat  = 0.3
	)

	var cpuRisk float64
	if cpuPressure <= cpuNormalLine {
		cpuRisk = 0
	} else if cpuPressure <= cpuWarnLine {
		normal := cpuWarnLine - cpuNormalLine
		over := cpuPressure - cpuNormalLine
		cpuRisk = math.Pow(over/normal, 1.3)
	} else {
		range_ := cpuMaxLine - cpuWarnLine
		over := cpuPressure - cpuWarnLine
		cpuRisk = 0.4 + 0.6*math.Pow(over/range_, 1.8)
	}
	if cpuRisk > 1.0 {
		cpuRisk = 1.0
	}

	var lossRisk float64
	if loss >= 1.0 {
		lossRisk = 1.0
	} else {
		x := lossSharpness * (loss - lossInflection)
		lossRisk = 1.0 / (1.0 + math.Exp(-x))
	}

	var latRisk float64
	switch {
	case latency <= latencyWarn:
		latRisk = 0
	case latency <= latencyCritical:
		norm := latencyCritical - latencyWarn
		over := latency - latencyWarn
		latRisk = math.Pow(over/norm, 1.5)
	case latency <= latencyMax:
		norm := latencyMax - latencyCritical
		over := latency - latencyCritical
		latRisk = 0.5 + 0.5*math.Pow(over/norm, 2.0)
	default:
		latRisk = 1.0
	}
	if latRisk > 1.0 {
		latRisk = 1.0
	}

	totalRisk := wCPU*cpuRisk + wLoss*lossRisk + wLat*latRisk
	if totalRisk > 1.0 {
		totalRisk = 1.0
	}

	l.Info("EdgeRisk result", slog.String("pre", pre),
		slog.Float64("cpuRisk", cpuRisk),
		slog.Float64("lossRisk", lossRisk),
		slog.Float64("latRisk", latRisk),
		slog.Float64("totalRisk", totalRisk))

	return totalRisk
}
