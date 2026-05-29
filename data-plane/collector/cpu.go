package collector

import (
	"fmt"
	model "data-plane/report-info"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/process"
)

const (
	sampleInterval = 1 * time.Second
	clkTck        = 100 // USER_HZ on Linux, ticks per second
)

var (
	processName = "data-proxy" // target process name

	physicalCores int
	logicalCores  int

	// per-process CPU tracking: PID → last sample {utime, stime, timestamp}
	procMu     sync.Mutex
	procLast   = map[int32]struct{ utime, stime uint64; ts time.Time }{}
	procPIDs   []int32 // cached PIDs matching processName

	peakMu      sync.Mutex
	prevUsage   float64
	currUsage   float64
	maxDelta    float64
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
	percent, err := getProcessCPU()
	if err != nil {
		return
	}

	peakMu.Lock()
	defer peakMu.Unlock()

	currUsage = percent

	if !firstSample {
		d := math.Abs(percent - prevUsage)
		if d > maxDelta {
			maxDelta = d
		}
	} else {
		firstSample = false
	}

	prevUsage = percent
}

// getProcessCPU reads /proc/[pid]/stat for all data-proxy processes and computes
// CPU% the same way "top" does: Δ(utime+stime) / Δ(wall time) / clkTck * 100.
// This avoids gopsutil's CPUPercentWithContext which has unreliable sampling intervals.
func getProcessCPU() (float64, error) {
	now := time.Now()

	// Resolve PIDs lazily or on process restart
	if len(procPIDs) == 0 {
		procPIDs = findProcessPIDs(processName)
	}
	if len(procPIDs) == 0 {
		return 0, fmt.Errorf("process %s not found", processName)
	}

	var totalCPU float64

	procMu.Lock()
	defer procMu.Unlock()

	// Prune dead PIDs from cache
	alive := procPIDs[:0]
	for _, pid := range procPIDs {
		utime, stime, err := readProcStat(pid)
		if err != nil {
			delete(procLast, pid)
			continue
		}
		alive = append(alive, pid)

		prev, ok := procLast[pid]
		if ok && prev.ts.Unix() > 0 {
			// CPU% = Δticks / Δseconds / clkTck * 100
			deltaTicks := (utime + stime) - (prev.utime + prev.stime)
			deltaSec := now.Sub(prev.ts).Seconds()
			if deltaSec > 0 {
				cpuPct := float64(deltaTicks) / deltaSec / clkTck * 100
				totalCPU += cpuPct
			}
		}

		procLast[pid] = struct{ utime, stime uint64; ts time.Time }{utime, stime, now}
	}
	procPIDs = alive

	return totalCPU, nil
}

// findProcessPIDs returns all PIDs matching the given process name.
func findProcessPIDs(name string) []int32 {
	procs, err := process.Processes()
	if err != nil {
		return nil
	}
	var pids []int32
	for _, p := range procs {
		pname, err := p.Name()
		if err != nil {
			continue
		}
		if strings.EqualFold(pname, name) {
			pids = append(pids, p.Pid)
		}
	}
	return pids
}

// readProcStat reads /proc/[pid]/stat and returns fields 14 (utime) and 15 (stime).
func readProcStat(pid int32) (utime, stime uint64, err error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, err
	}
	// /proc/[pid]/stat: fields are space-separated, but field 2 (comm) may contain
	// spaces inside parentheses. Find the closing ')' then split the rest.
	s := string(data)
	closeParen := strings.LastIndex(s, ")")
	if closeParen < 0 {
		return 0, 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(s[closeParen+2:]) // fields after ") "
	// fields[11] = utime, fields[12] = stime (0-indexed after ") ")
	if len(fields) < 13 {
		return 0, 0, fmt.Errorf("too few fields in /proc/%d/stat", pid)
	}
	utime, err = strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	stime, err = strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return utime, stime, nil
}

// collectCPU returns the latest sampled CPU usage and delta, then resets the window.
// Returns currUsage (instantaneous, same source as "top") rather than a window peak,
// so the routing algorithms see a value close to real-time CPU.
func collectCPU() (model.CPUInfo, error) {
	peakMu.Lock()
	usage := currUsage
	delta := maxDelta

	// reset window
	maxDelta = 0
	firstSample = true
	peakMu.Unlock()

	if usage <= 1 {
		usage = 1
	}

	return model.CPUInfo{
		PhysicalCore: physicalCores,
		LogicalCore:  logicalCores,
		Usage:        usage,
		Load1Min:     0,
		LoadDelta:    delta,
	}, nil
}
