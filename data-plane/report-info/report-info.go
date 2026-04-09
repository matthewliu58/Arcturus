package report_info

import (
	"data-plane/probing"
	"time"
)

// VMReport 数据平面上报给控制平面的核心结构体（全量字段）
type VMReport struct {
	// 基础标识（必选）
	VMID        string    `json:"vm_id"`        // VM唯一标识（UUID/主机名+MAC，全局唯一）
	CollectTime time.Time `json:"collect_time"` // 采集时间（UTC时区，格式：2025-12-20T10:00:00Z）
	ReportID    string    `json:"report_id"`    // 单条上报记录ID（UUID，数据平面生成/控制平面补全）

	// CPU信息（必选）
	CPU CPUInfo `json:"cpu"`

	// 内存信息（必选）
	Memory MemoryInfo `json:"memory"`

	// 磁盘信息（可选，默认采集根分区/系统盘）
	Disk DiskInfo `json:"disk,omitempty"`

	// 网络信息（必选，外网IP为核心字段）
	Network NetworkInfo `json:"network"`

	// 系统基础信息（可选）
	OS OSInfo `json:"os,omitempty"`

	// 进程信息（可选）
	Process ProcessInfo `json:"process,omitempty"`

	LinksCongestion []LinkCongestionInfo `json:"links_congestion"`
}

// CPUInfo CPU维度信息
type CPUInfo struct {
	PhysicalCore int     `json:"physical_core"`       // 物理核数（如2，无则填0）
	LogicalCore  int     `json:"logical_core"`        // 逻辑核数（如4，无则填0）
	Usage        float64 `json:"usage"`               // CPU整体使用率（%，保留1位小数，如25.3）
	Load1Min     float64 `json:"load_1min,omitempty"` // 1分钟系统负载均值（可选，如0.8）
}

// MemoryInfo 内存维度信息（单位：字节）
type MemoryInfo struct {
	Total uint64  `json:"total"` // 总内存（如17179869184 = 16GB）
	Used  uint64  `json:"used"`  // 已用内存
	Free  uint64  `json:"free"`  // 可用内存
	Usage float64 `json:"usage"` // 内存使用率（%，保留1位小数，如60.5）
}

// DiskInfo 磁盘维度信息（默认采集根分区，单位：字节）
type DiskInfo struct {
	Total uint64  `json:"total"` // 分区总大小
	Used  uint64  `json:"used"`  // 已用大小
	Free  uint64  `json:"free"`  // 可用大小
	Usage float64 `json:"usage"` // 使用率（%，保留1位小数）
	Path  string  `json:"path"`  // 挂载路径（如/、C:\）
}

// NetworkInfo 网络维度信息（外网IP为核心必选）
type NetworkInfo struct {
	PublicIP   string `json:"public_ip"`             // 外网IP（必选，无则填"no-public-ip"，多IP用逗号分隔）
	PrivateIP  string `json:"private_ip"`            // 内网IP（如192.168.1.100，无则填空）
	PortCount  int    `json:"port_count"`            // 已占用端口总数（TCP+UDP，无则填0）
	TrafficIn  uint64 `json:"traffic_in,omitempty"`  // 5秒采集周期内入网卡总流量（字节，可选）
	TrafficOut uint64 `json:"traffic_out,omitempty"` // 5秒采集周期内出网卡总流量（字节，可选）
}

// OSInfo 系统基础信息
type OSInfo struct {
	OSName    string `json:"os_name"`    // 操作系统（如Linux/Windows/macOS）
	KernelVer string `json:"kernel_ver"` // 内核版本（如Linux 5.15.0-78-generic/Windows 10 22H2）
	Hostname  string `json:"hostname"`   // 主机名
	BootTime  int64  `json:"boot_time"`  // 系统启动时间戳（秒，无则填0）
}

// ProcessInfo 进程信息
type ProcessInfo struct {
	ActiveCount int             `json:"active_count"` // 活跃进程数（无则填0）
	TopCPU      []ProcessDetail `json:"top_cpu"`      // CPU占用TOP3进程（不足3条补空）
	TopMem      []ProcessDetail `json:"top_mem"`      // 内存占用TOP3进程（不足3条补空）
}

// ProcessDetail 进程详情
type ProcessDetail struct {
	PID   int     `json:"pid"`   // 进程ID（无则填0）
	Name  string  `json:"name"`  // 进程名（如nginx/java，无则填空）
	Usage float64 `json:"usage"` // 占用率（%，保留1位小数）
}

type ApiResponse struct {
	Code int         `json:"code"` // 200=成功，400=参数错误，500=服务端错误
	Msg  string      `json:"msg"`  // 提示信息
	Data interface{} `json:"data"` // 业务数据：上报时放VMReport，响应时放回填充后的VMReport
}

type LinkCongestionInfo struct {
	TargetIP       string            `json:"target_ip"` // 目标节点 IP
	Target         probing.ProbeTask `json:"target"`
	PacketLoss     float64           `json:"packet_loss"`     // 丢包率，百分比
	AverageLatency float64           `json:"average_latency"` // 平均延迟（毫秒）
}
