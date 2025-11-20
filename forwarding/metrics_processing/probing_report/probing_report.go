package probing_report

import (
	"forwarding/common"
	"forwarding/metrics_processing/protocol"
	"forwarding/metrics_processing/storage"
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// ProbeTimeout is the maximum time to wait for a TCP probing_report
var ProbeTimeout = 2 * time.Second

func performTCPProbe(targetIP string, port string) (int64, error) {
	timeoutDuration := ProbeTimeout
	startTime := time.Now()
	conn, err := net.DialTimeout("tcp", targetIP+":"+port, timeoutDuration)
	if err != nil {
		return 5001, err
	}
	defer conn.Close()
	// Calculate the delay.
	delay := time.Since(startTime).Milliseconds()

	// Check if the delay is less than 5ms.
	if delay < 5 {
		return 5, nil
	}
	return delay, nil
}

// If you have a large number of detection nodes, you can use this method to compress the data before uploading it;
// otherwise, delegate the decision-making authority to the controller.
/*func processRegionProbeResults(probes []*protocol.ProbeResult) ([]*protocol.ProbeResult, error) {
	if len(probes) == 0 {
		return []*protocol.ProbeResult{}, nil
	}

	delays := make([]float64, 0, len(probes))
	ipToIndex := make(map[int]string)
	indexToIP := make(map[string]int)

	for _, probing_report := range probes {
		if probing_report.TcpDelay > 0 {
			delays = append(delays, float64(probing_report.TcpDelay))
			ipToIndex[len(delays)-1] = probing_report.TargetIp
			indexToIP[probing_report.TargetIp] = len(delays) - 1
		}
	}

	if len(delays) == 0 {
		return probes, nil // ，
	}

	k := 3
	sensitivity := 1.5
	outliers := system_optimization.DetectOutliersAdaptive(delays, k, sensitivity)

	outlierIndices := make(map[int]bool)
	for _, outlier := range outliers {
		outlierIndices[outlier.Index] = true
	}

	var sumNormal float64
	var countNormal int

	for i, delay := range delays {
		if !outlierIndices[i] {
			sumNormal += delay
			countNormal++
		}
	}

	var avgNormal float64
	if countNormal > 0 {
		avgNormal = sumNormal / float64(countNormal)
	}

	optimizedProbes := make([]*protocol.ProbeResult, 0)
	if countNormal > 0 {
		normalProbe := &protocol.ProbeResult{
			TargetIp: "normal_avg", //
			TcpDelay: int64(math.Round(avgNormal)),
		}
		optimizedProbes = append(optimizedProbes, normalProbe)

	}
	for _, outlier := range outliers {
		targetIP := ipToIndex[outlier.Index]
		outlierProbe := &protocol.ProbeResult{
			TargetIp: targetIP,
			TcpDelay: int64(math.Round(outlier.Value)),
		}
		optimizedProbes = append(optimizedProbes, outlierProbe)
	}

	return optimizedProbes, nil
}*/

func CollectRegionProbeResults(fileManager *storage.FileManager, sourceIp string) ([]*protocol.RegionProbeResult, error) {
	nodeList := fileManager.GetNodeList()
	probeTasks := fileManager.GetProbeTasks()
	if nodeList == nil || len(nodeList.Nodes) == 0 {

		return []*protocol.RegionProbeResult{}, nil
	}
	if len(probeTasks) == 0 {

		return []*protocol.RegionProbeResult{}, nil
	}
	ipToRegion := make(map[string]string)
	for _, node := range nodeList.Nodes {
		ipToRegion[node.Ip] = node.Region
	}
	regionProbesMap := make(map[string][]*protocol.ProbeResult)
	var resultsLock sync.Mutex
	var wg sync.WaitGroup
	poolConfig := common.PoolConfig{
		MaxWorkers: 50,
	}
	pool, err := common.NewPool(poolConfig)
	if err != nil {
		log.Printf(": %v", err)
		return nil, err
	}
	defer pool.Release()
	log.Infof("the number of probing_report task %d", len(probeTasks))
	for _, task := range probeTasks {
		wg.Add(1)
		taskCopy := task
		if err := pool.Submit(func() {
			defer wg.Done()
			targetRegion, exists := ipToRegion[taskCopy.TargetIp]
			if !exists {
				log.Warningf("the IP: %s cant find specific region", taskCopy.TargetIp)
				targetRegion = "unknown"
			}
			tcpDelay, err := performTCPProbe(taskCopy.TargetIp, "50051")
			if err != nil {
				log.Warningf("the probing_report failed, target ip: %s, err: %v", taskCopy.TargetIp, err)
				return
			}
			probeResult := &protocol.ProbeResult{
				TargetIp: taskCopy.TargetIp,
				TcpDelay: tcpDelay,
			}
			resultsLock.Lock()
			regionProbesMap[targetRegion] = append(regionProbesMap[targetRegion], probeResult)
			resultsLock.Unlock()
			log.Infof("Probe successful for %s (from region %s): %dms",
				taskCopy.TargetIp, targetRegion, tcpDelay)
		}); err != nil {
			// Submit failed — compensate wg and log. This avoids wg.Wait deadlock.
			wg.Done()
			log.Warningf("failed to submit probing_report task for %s: %v", taskCopy.TargetIp, err)
			continue
		}
	}
	// --- New probing_report logic for DomainIPMappings ---
	//domainMappings := routing.GetAllDomainMapIP()
	domainMappings := fileManager.GetDomainIPMappings()
	if domainMappings != nil {
		for _, mapping := range domainMappings {
			if mapping.Ip == "" { // Skip if IP is empty
				log.Warningf("Skipping domain mapping for %s, IP is empty.", mapping.Domain)
				continue
			}
			wg.Add(1)
			mappingCopy := mapping // Capture loop variable for goroutine
			if err := pool.Submit(func() {
				defer wg.Done()
				targetIP := mappingCopy.Ip
				// change source ip
				//sourceIp, _ := collector.GetIP()
				sourceRegion, exists := ipToRegion[sourceIp]
				if !exists {
					log.Warningf("Region lookup: IP %s (from domain %s), Region unknown", targetIP, mappingCopy.Domain)
					sourceRegion = "unknown"
				}
				tcpDelay, probeErr := performTCPProbe(targetIP, "80")
				if probeErr != nil {
					log.Warningf("TCP Probe Error for %s (from domain %s): %v", targetIP, mappingCopy.Domain, probeErr)
					return
				}
				probeResult := &protocol.ProbeResult{
					TargetIp: targetIP,
					TcpDelay: tcpDelay,
				}
				resultsLock.Lock()
				regionProbesMap[sourceRegion] = append(regionProbesMap[sourceRegion], probeResult)
				resultsLock.Unlock()
				log.Infof("Probe successful for %s (from domain %s, region %s): %dms",
					targetIP, mappingCopy.Domain, sourceRegion, tcpDelay)
			}); err != nil {
				wg.Done()
				log.Warningf("failed to submit domain mapping probing_report for %s: %v", mappingCopy.Domain, err)
				continue
			}
		}
	}
	wg.Wait()
	log.Infof("All probes finished. Aggregating results...")

	var regionProbeResults []*protocol.RegionProbeResult
	resultsLock.Lock()
	for region, probes := range regionProbesMap {
		if len(probes) == 0 {
			log.Warningf("No successful probes for region: %s, skipping.", region)
			continue
		}
		regionResult := &protocol.RegionProbeResult{
			Region:   region,
			IpProbes: probes,
		}
		regionProbeResults = append(regionProbeResults, regionResult)
		log.Infof("Aggregated %d probes for region: %s", len(probes), region)
	}
	resultsLock.Unlock()
	log.Infof("Total regions with probing_report results: %d", len(regionProbeResults))
	return regionProbeResults, nil
}
