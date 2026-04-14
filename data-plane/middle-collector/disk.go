package middle_collector

import (
	model "data-plane/report-info"
	"github.com/shirou/gopsutil/v3/disk"
)

// collectDisk 采集根分区/系统盘信息
func collectDisk() (model.DiskInfo, error) {
	// 默认采集根分区（Linux: /, Windows: C:\）
	path := "/"
	// Windows适配
	// if runtime.GOOS == "windows" {
	// 	path = "C:\\"
	// }

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
