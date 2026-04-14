package middle_collector

import (
	model "data-plane/report-info"
	"sort"

	"github.com/shirou/gopsutil/v3/process"
)

// collectProcess 采集进程信息（活跃数、TOP3 CPU/内存进程）
func collectProcess() (model.ProcessInfo, error) {
	// 1. 获取所有进程
	processes, err := process.Processes()
	if err != nil {
		return model.ProcessInfo{}, err
	}
	activeCount := len(processes)

	// 2. 筛选TOP3 CPU进程
	cpuProcesses := make([]model.ProcessDetail, 0)
	for _, p := range processes {
		name, _ := p.Name()
		cpu, _ := p.CPUPercent()
		if name == "" || cpu == 0 {
			continue
		}
		cpuProcesses = append(cpuProcesses, model.ProcessDetail{
			PID:   int(p.Pid),
			Name:  name,
			Usage: cpu,
		})
	}
	// 按CPU使用率排序
	sort.Slice(cpuProcesses, func(i, j int) bool {
		return cpuProcesses[i].Usage > cpuProcesses[j].Usage
	})
	// 取前3个
	if len(cpuProcesses) > 3 {
		cpuProcesses = cpuProcesses[:3]
	}

	// 3. 筛选TOP3 内存进程
	memProcesses := make([]model.ProcessDetail, 0)
	for _, p := range processes {
		name, _ := p.Name()
		mem, _ := p.MemoryPercent()
		if name == "" || mem == 0 {
			continue
		}
		memProcesses = append(memProcesses, model.ProcessDetail{
			PID:   int(p.Pid),
			Name:  name,
			Usage: float64(mem),
		})
	}
	// 按内存使用率排序
	sort.Slice(memProcesses, func(i, j int) bool {
		return memProcesses[i].Usage > memProcesses[j].Usage
	})
	// 取前3个
	if len(memProcesses) > 3 {
		memProcesses = memProcesses[:3]
	}

	return model.ProcessInfo{
		ActiveCount: activeCount,
		TopCPU:      cpuProcesses,
		TopMem:      memProcesses,
	}, nil
}
