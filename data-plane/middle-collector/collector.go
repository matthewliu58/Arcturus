package middle_collector

import (
	model "data-plane/report-info"
	"log/slog"
	"time"
)

type VMCollector struct{}

func NewVMCollector() *VMCollector {
	return &VMCollector{}
}

func (c *VMCollector) Collect(pre string, logger *slog.Logger) (*model.VMReport, error) {

	cpuInfo, err := collectCPU()
	if err != nil {
		return nil, err
	}

	memoryInfo, err := collectMemory()
	if err != nil {
		return nil, err
	}

	diskInfo, err := collectDisk()
	if err != nil {
		return nil, err
	}

	networkInfo, err := collectNetwork()
	if err != nil {
		return nil, err
	}

	osInfo, err := collectOS()
	if err != nil {
		return nil, err
	}

	processInfo, err := collectProcess()
	if err != nil {
		return nil, err
	}

	linksCong := BuildLinkCongestion()

	//hostname, _ := os.Hostname()

	val := &model.VMReport{
		VMID:            "vm-" + networkInfo.PublicIP + "-001",
		CollectTime:     time.Now().UTC(),
		ReportID:        "",
		CPU:             cpuInfo,
		Memory:          memoryInfo,
		Disk:            diskInfo,
		Network:         networkInfo,
		OS:              osInfo,
		Process:         processInfo,
		LinksCongestion: linksCong,
	}

	logger.Info("Collect", slog.String("pre", pre), slog.Any("val", val))

	return val, nil
}
