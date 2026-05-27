package collector

import (
	"context"
	model "data-plane/report-info"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/process"
)

const (
	sampleInterval = 1 * time.Second
)

var (
	processName = "data-proxy" // target process name, empty = system-wide

	physicalCores int
	logicalCores  int

	peakMu      sync.Mutex
	peakUsage   float64
	maxDelta    float64
	prevUsage   float64
	firstSample bool = true

	startOnce sync.Once
	stopCh    chan struct{}
)

// StartCPUSampler starts background CPU sampling
func StartCPUSampler() {
	startOnce.Do(func() {
		// cache core counts (process/system mode)
		physicalCores, _ = cpu.Counts(false)
		logicalCores, _ = cpu.Counts(true)

		stopCh = make(chan struct{})
		go runSampler()
	})
}

// StopCPUSampler stops sampling
func StopCPUSampler() {
	peakMu.Lock()
	defer peakMu.Unlock()

	if stopCh != nil {
		close(stopCh)
		stopCh = nil
	}
}

func runSampler() {
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	prevUsage = 0.0
	firstSample = true

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			sample()
		}
	}
}

func sample() {
	var percent float64
	var err error

	if processName == "" {
		percent, err = getSystemCPU()
	} else {
		percent, err = getProcessCPU()
	}

	if err != nil {
		return
	}

	// update peak and delta
	peakMu.Lock()
	if percent > peakUsage {
		peakUsage = percent
	}

	// skip first sample for delta calculation
	if !firstSample {
		d := math.Abs(percent - prevUsage)
		if d > maxDelta {
			maxDelta = d
		}
	} else {
		firstSample = false
	}

	prevUsage = percent
	peakMu.Unlock()
}

// getSystemCPU: returns total CPU% (can exceed 100% on multi-core)
func getSystemCPU() (float64, error) {
	percent, err := cpu.Percent(0, true)
	if err != nil || len(percent) == 0 {
		return 0, err
	}

	total := 0.0
	for _, p := range percent {
		total += p
	}
	return total, nil
}

// getProcessCPU: stable sampling + case-insensitive match
func getProcessCPU() (float64, error) {
	procs, err := process.Processes()
	if err != nil {
		return 0, err
	}

	var totalPercent float64
	count := 0

	for _, p := range procs {
		name, err := p.Name()
		if err != nil {
			continue
		}

		// case-insensitive match
		if strings.EqualFold(name, processName) {
			// interval-based sampling for accurate values
			cpuPercent, err := p.CPUPercentWithContext(context.Background())
			if err != nil {
				continue
			}
			totalPercent += cpuPercent
			count++
		}
	}

	if count == 0 {
		return 0, os.ErrNotExist
	}

	return totalPercent, nil
}

// collectCPU collects and resets window
func collectCPU() (model.CPUInfo, error) {
	peakMu.Lock()
	usage := peakUsage
	delta := maxDelta

	// reset window
	peakUsage = 0
	maxDelta = 0
	firstSample = true
	peakMu.Unlock()

	return model.CPUInfo{
		PhysicalCore: physicalCores,
		LogicalCore:  logicalCores,
		Usage:        usage,
		Load1Min:     0,
		LoadDelta:    delta,
	}, nil
}
