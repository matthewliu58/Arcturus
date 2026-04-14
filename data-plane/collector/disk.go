package collector

import (
	model "data-plane/report-info"
	"github.com/shirou/gopsutil/v3/disk"
)

func collectDisk() (model.DiskInfo, error) {

	path := "/"

	stat, err := disk.Usage(path)
	if err != nil {
		return model.DiskInfo{}, err
	}

	return model.DiskInfo{
		Total: stat.Total,
		Used:  stat.Used,
		Free:  stat.Free,
		Usage: stat.UsedPercent,
		Path:  path,
	}, nil
}
