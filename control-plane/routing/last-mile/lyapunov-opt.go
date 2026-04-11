package last_mile

import (
	agg "control-plane/info-agg"
	rece "control-plane/receive-info"
	"control-plane/routing/routing"
	"log/slog"
)

type LyapunovSolver struct {
	edgeAggs     map[string]*rece.LastStatsValue
	nodes        map[string]*agg.Telemetry
	nodeLocation map[string][]string
}

// 创建实例
func NewLyapunovSolver(edgeAggs map[string]*rece.LastStatsValue, nodes map[string]*agg.Telemetry,
	nodeLocation map[string][]string) *LyapunovSolver {
	return &LyapunovSolver{
		edgeAggs:     edgeAggs,
		nodes:        nodes,
		nodeLocation: nodeLocation,
	}
}

func (l *LyapunovSolver) Computing(start, end, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	return nil, nil
}
