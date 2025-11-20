package main

import (
	"fmt"
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// ProbeResult represents a single probing_report result (internal use)
type ProbeResult struct {
	TargetIP string
	DelayMs  float64
	Success  bool
}

// PerformTCPProbe performs a TCP connection probing_report to measure delay
// Returns delay in milliseconds
// This is similar to performTCPProbe in forwarding/metrics_processing/probing_report/probing_report.go
func PerformTCPProbe(targetIP string, port int, timeoutMs int) (float64, error) {
	timeout := time.Duration(timeoutMs) * time.Millisecond
	target := fmt.Sprintf("%s:%d", targetIP, port)

	startTime := time.Now()

	conn, err := net.DialTimeout("tcp", target, timeout)
	if err != nil {
		log.Debugf("TCP probing_report failed for %s: %v", target, err)
		return 0, fmt.Errorf("connection failed: %w", err)
	}
	defer conn.Close()

	delay := float64(time.Since(startTime).Microseconds()) / 1000.0
	log.Debugf("TCP probing_report successful for %s: %.2fms", target, delay)

	return delay, nil
}

// ProbeAllNodes performs concurrent TCP probes to all target nodes
func ProbeAllNodes(nodes []NodeInfo, timeoutMs int) []ProbeResult {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []ProbeResult
	)

	log.Infof("Starting probing_report for %d nodes...", len(nodes))

	for _, node := range nodes {
		wg.Add(1)

		// Capture node for goroutine
		nodeCopy := node

		go func() {
			defer wg.Done()

			delay, err := PerformTCPProbe(nodeCopy.IP, nodeCopy.Port, timeoutMs)

			result := ProbeResult{
				TargetIP: nodeCopy.IP,
				DelayMs:  delay,
				Success:  err == nil,
			}

			mu.Lock()
			results = append(results, result)
			mu.Unlock()

			if err != nil {
				log.Warnf("Probe failed for %s:%d - %v", nodeCopy.IP, nodeCopy.Port, err)
			} else {
				log.Infof("Probe succeeded for %s:%d - %.2fms", nodeCopy.IP, nodeCopy.Port, delay)
			}
		}()
	}

	wg.Wait()

	successCount := countSuccessful(results)
	log.Infof("Probe completed: %d total, %d succeeded, %d failed",
		len(results), successCount, len(results)-successCount)

	return results
}

// countSuccessful counts the number of successful probes
func countSuccessful(results []ProbeResult) int {
	count := 0
	for _, r := range results {
		if r.Success {
			count++
		}
	}
	return count
}

// FilterSuccessful filters only successful probing_report results and converts to DelayInfo
func FilterSuccessful(results []ProbeResult) []DelayInfo {
	var delays []DelayInfo
	timestamp := time.Now().Unix()

	for _, result := range results {
		if result.Success {
			delays = append(delays, DelayInfo{
				TargetIP:  result.TargetIP,
				DelayMs:   result.DelayMs,
				Timestamp: timestamp,
			})
		}
	}

	return delays
}
