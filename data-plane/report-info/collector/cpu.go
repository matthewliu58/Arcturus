package collector

import (
	model "data-plane/report-info"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
)

// collectCPU 采集CPU信息
// collectCPU 采集CPU信息（兼容gopsutil v3+跨平台）
func collectCPU() (model.CPUInfo, error) {
	// 1. 获取CPU核心数
	cpuCounts, err := cpu.Counts(true) // 逻辑核数
	if err != nil {
		return model.CPUInfo{}, err
	}
	physicalCounts, err := cpu.Counts(false) // 物理核数
	if err != nil {
		return model.CPUInfo{}, err
	}

	// 2. 获取CPU使用率（采样1秒）
	percent, err := cpu.Percent(1*time.Second, false)
	if err != nil {
		return model.CPUInfo{}, err
	}
	usage := 0.0
	if len(percent) > 0 {
		usage = percent[0]
	}

	// 3. 获取系统负载（仅Linux/macOS支持，Windows返回0）
	var load1Min float64 = 0.0
	loadStat, err := load.Avg() // v3版本用 load.Avg() 替代 cpu.LoadAvg()
	if err == nil {             // 仅当无错误时赋值（Windows会报错，直接用0）
		load1Min = loadStat.Load1
	}

	return model.CPUInfo{
		PhysicalCore: physicalCounts,
		LogicalCore:  cpuCounts,
		Usage:        usage,
		Load1Min:     load1Min, // 1分钟负载均值
	}, nil
}
