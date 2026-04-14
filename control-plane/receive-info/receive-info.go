package receive_info

import (
	"time"
)

type VMReport struct {
	VMID            string               `json:"vm_id"`
	CollectTime     time.Time            `json:"collect_time"`
	ReportID        string               `json:"report_id"`
	CPU             CPUInfo              `json:"cpu"`
	Memory          MemoryInfo           `json:"memory"`
	Disk            DiskInfo             `json:"disk,omitempty"`
	Network         NetworkInfo          `json:"network"`
	OS              OSInfo               `json:"os,omitempty"`
	Process         ProcessInfo          `json:"process,omitempty"`
	LinksCongestion []LinkCongestionInfo `json:"links_congestion"`
}

type CPUInfo struct {
	PhysicalCore int     `json:"physical_core"`
	LogicalCore  int     `json:"logical_core"`
	Usage        float64 `json:"usage"`
	Load1Min     float64 `json:"load_1min,omitempty"`
	LoadDelta    float64 `json:"load_delta,omitempty"`
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
	TrafficIn  uint64 `json:"traffic_in,omitempty"`
	TrafficOut uint64 `json:"traffic_out,omitempty"`
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

type LinkCongestionInfo struct {
	TargetIP       string    `json:"target_ip"`
	Target         ProbeTask `json:"target"`
	PacketLoss     float64   `json:"packet_loss"`
	AverageLatency float64   `json:"average_latency"`
}

type LastStatsKey struct {
	Continent string `json:"continent"`
	Country   string `json:"country"`
	Province  string `json:"province"`
	City      string `json:"city"`
	ISP       string `json:"isp"`
}

type LastStatsVal struct {
	Count int     `json:"count"`
	SumRT float64 `json:"sum_rt"`
	AvgRT float64 `json:"avg_rt"`
	P95RT int     `json:"p95_rt"`
}

type LastStats struct {
	DelayStats map[LastStatsKey]*LastStatsVal `json:"delay_stats"`
	IP         string                         `json:"ip"`
	ISP        string                         `json:"isp"`
	Continent  string                         `json:"continent"`
	Country    string                         `json:"country"`
	Province   string                         `json:"province"`
	City       string                         `json:"city"`
}
