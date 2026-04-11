package last_mile

import (
	agg "control-plane/info-agg"
	rece "control-plane/receive-info"
	"control-plane/routing/routing"
	"log/slog"
)

//type LastStatsVal struct {
//	Count int     `json:"count"`
//	SumRT float64 `json:"sum_rt"`
//	AvgRT float64 `json:"avg_rt"`
//	P95RT int     `json:"p95_rt"`
//}

//type CPUInfo struct {
//	PhysicalCore int     `json:"physical_core"`        // 物理核数（如2，无则填0）
//	LogicalCore  int     `json:"logical_core"`         // 逻辑核数（如4，无则填0）
//	Usage        float64 `json:"usage"`                // CPU整体使用率（%，保留1位小数，如25.3）
//	Load1Min     float64 `json:"load_1min,omitempty"`  // 1分钟系统负载均值（可选，如0.8）
//	LoadDelta    float64 `json:"load_delta,omitempty"` // 负载绝对变化量
//}

//type Telemetry struct {
//	PublicIP        string                        `json:"public_ip"` // 节点公网 IP
//	Provider        string                        `json:"provider"`  // 云厂商
//	Continent       string                        `json:"continent"` // 所属大洲
//	Country         string                        `json:"country"`   // 国家
//	City            string                        `json:"city"`      // 城市
//	CpuPressure     float64                       `json:"cpu_pressure"`
//	Cpu             rece.CPUInfo                  `json:"cpu"`
//}

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

func (l *LyapunovSolver) Computing(endPoints routing.EndPoints,
	pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	return nil, nil
}
