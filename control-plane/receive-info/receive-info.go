package receive_info

import (
	"time"
)

type VMReport struct {
	VMID            string           `json:"vm_id"`
	CollectTime     time.Time        `json:"collect_time"`
	ReportID        string           `json:"report_id"`
	CPU             CPUInfo          `json:"cpu"`
	Memory          MemoryInfo       `json:"memory"`
	Disk            DiskInfo         `json:"disk"`
	Network         NetworkInfo      `json:"network"`
	OS              OSInfo           `json:"os"`
	Process         ProcessInfo      `json:"process"`
	LinksCongestion []LinkCongestion `json:"links_congestion"`
}

type CPUInfo struct {
	PhysicalCore int     `json:"physical_core"`
	LogicalCore  int     `json:"logical_core"`
	Usage        float64 `json:"usage"`
	Load1Min     float64 `json:"load_1min"`
	LoadDelta    float64 `json:"load_delta"`
}

type MemoryInfo struct {
	Total uint64  `json:"total"`
	Used  uint64  `json:"used"`
	Free  uint64  `json:"free"`
	Usage float64 `json:"usage"`
}

type DiskInfo struct {
	Total uint64  `json:"total"`
	Used  uint64  `json:"used"`
	Free  uint64  `json:"free"`
	Usage float64 `json:"usage"`
	Path  string  `json:"path"`
}

type NetworkInfo struct {
	PublicIP   string `json:"public_ip"`
	PrivateIP  string `json:"private_ip"`
	PortCount  int    `json:"port_count"`
	TrafficIn  uint64 `json:"traffic_in"`
	TrafficOut uint64 `json:"traffic_out"`
}

type OSInfo struct {
	OSName    string `json:"os_name"`
	KernelVer string `json:"kernel_ver"`
	Hostname  string `json:"hostname"`
	BootTime  int64  `json:"boot_time"`
}

type ProcessInfo struct {
	ActiveCount int             `json:"active_count"`
	TopCPU      []ProcessDetail `json:"top_cpu"`
	TopMem      []ProcessDetail `json:"top_mem"`
}

type EnvoyBufferStats struct {
	TotalBuffer   int64  `json:"total_buffer"`
	ActiveConnRaw string `json:"active_conn_raw"`
	ActiveConn    int64  `json:"active_conn"`
	PerConnBuffer int64  `json:"per_conn_buffer"`
}

type ProcessDetail struct {
	PID   int     `json:"pid"`
	Name  string  `json:"name"`
	Usage float64 `json:"usage"`
}

type ApiResponse struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

type ProbeTask struct {
	TargetType string `json:"TargetType"`
	Provider   string `json:"Provider"`
	IP         string `json:"IP"`
	Port       int    `json:"Port"`
	Region     string `json:"Region"`
	ID         string `json:"ID"`
}

type LinkCongestion struct {
	TargetIP       string    `json:"target_ip"`
	Target         ProbeTask `json:"target"`
	PacketLoss     float64   `json:"packet_loss"`
	AverageLatency float64   `json:"average_latency"`
}

type LastKey struct {
	Continent string `json:"continent"`
	Country   string `json:"country"`
	City      string `json:"city"`
}

type LastCongestion struct {
	Count int     `json:"count"`
	SumRT float64 `json:"sum_rt"`
	AvgRT float64 `json:"avg_rt"`
	P95RT int     `json:"p95_rt"`
}

type LastTelemetry struct {
	LastsCongestion map[string]*LastCongestion `json:"lasts_congestion"`
	IP              string                     `json:"ip"`
	Continent       string                     `json:"continent"`
	Country         string                     `json:"country"`
	City            string                     `json:"city"`
}
