package middle_collector

import (
	model "data-plane/report-info"
	"github.com/shirou/gopsutil/v3/mem"
)

func collectMemory() (model.MemoryInfo, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return model.MemoryInfo{}, err
	}

	return model.MemoryInfo{
		Total: v.Total,
		Used:  v.Used,
		Free:  v.Free,
		Usage: v.UsedPercent,
	}, nil
}
