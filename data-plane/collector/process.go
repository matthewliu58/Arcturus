package collector

import (
	model "data-plane/report-info"
	"sort"

	"github.com/shirou/gopsutil/v3/process"
)

func collectProcess() (model.ProcessInfo, error) {

	processes, err := process.Processes()
	if err != nil {
		return model.ProcessInfo{}, err
	}
	activeCount := len(processes)

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
	sort.Slice(cpuProcesses, func(i, j int) bool {
		return cpuProcesses[i].Usage > cpuProcesses[j].Usage
	})
	if len(cpuProcesses) > 3 {
		cpuProcesses = cpuProcesses[:3]
	}

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
	sort.Slice(memProcesses, func(i, j int) bool {
		return memProcesses[i].Usage > memProcesses[j].Usage
	})
	if len(memProcesses) > 3 {
		memProcesses = memProcesses[:3]
	}

	return model.ProcessInfo{
		ActiveCount: activeCount,
		TopCPU:      cpuProcesses,
		TopMem:      memProcesses,
	}, nil
}
