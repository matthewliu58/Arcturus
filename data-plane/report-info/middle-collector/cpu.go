package middle_collector

import (
	model "data-plane/report-info"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
)

const (
	cpuWindow = 60 * time.Second // 有效采样窗口，必须大于采样周期(10s)
)

var (
	lastCPU     model.CPUInfo
	lastCPUTime time.Time
	cpuMu       sync.Mutex
)

// collectCPU 采集CPU信息（兼容gopsutil v3+跨平台）
// 两次调用之间计算 UsageDelta 和 LoadDelta 绝对变化量
// 只有在有效采样窗口（10秒）内才计算变化量，避免冷启动冲击
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

	cpuMu.Lock()
	defer cpuMu.Unlock()

	now := time.Now()
	info := model.CPUInfo{
		PhysicalCore: physicalCounts,
		LogicalCore:  cpuCounts,
		Usage:        usage,
		Load1Min:     load1Min,
	}

	// 计算负载绝对变化量（捕捉瞬时冲击），仅在有效窗口内
	elapsed := now.Sub(lastCPUTime)
	if lastCPUTime.IsZero() || elapsed > cpuWindow {
		// 冷启动或超时，不计算变化量，Delta 保持 0
	} else {
		info.LoadDelta = load1Min - lastCPU.Load1Min
	}

	lastCPU = info
	lastCPUTime = now

	return info, nil
}
