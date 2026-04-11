package last_mile

import (
	agg "control-plane/info-agg"
	rece "control-plane/receive-info"
	"control-plane/routing/routing"
	"log/slog"
)

type LyapunovSolver struct {
	edgeAgg      map[string]*rece.LastStatsVal
	nodeTel      map[string]*agg.Telemetry
	nodeLocation map[string][]string
}

// 创建实例
func NewLyapunovSolver(edgeAgg map[string]*rece.LastStatsVal,
	nodeTel map[string]*agg.Telemetry, nodeLocation map[string][]string) *LyapunovSolver {
	return &LyapunovSolver{
		edgeAgg:      edgeAgg,
		nodeTel:      nodeTel,
		nodeLocation: nodeLocation,
	}
}

func (l *LyapunovSolver) Computing(start, end, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	return nil, nil
}
