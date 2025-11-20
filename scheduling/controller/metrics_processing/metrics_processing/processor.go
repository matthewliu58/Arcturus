package metrics_processing

import (
	"database/sql"
	log "github.com/sirupsen/logrus"
	pb "scheduling/controller/metrics_processing/protocol"
	"scheduling/db_models"
	"time"
)

type Processor struct {
	db *sql.DB
}

func NewProcessor(db *sql.DB) *Processor {
	return &Processor{db: db}
}

func (p *Processor) ProcessProbeResults(sourceIP string, results []*pb.RegionProbeResult) error {
	probeTime := time.Now()

	sourceRegion, err := db_models.GetNodeRegion(p.db, sourceIP)
	if err != nil {
		log.Errorf("Error getting source region for IP %s: %v. Using 'unknown'.", sourceIP, err)
		sourceRegion = "unknown"
	}
	log.Infof("Processing %d region results for source IP: %s (Region: %s)", len(results), sourceIP, sourceRegion)
	for _, regionResult := range results {
		targetRegion := regionResult.Region
		if targetRegion == "" {
			log.Warningf("Warning: Received probing_report result with empty target region for source IP %s. Setting to 'unknown'.", sourceIP)
			targetRegion = "unknown"
		}
		log.Infof("  Processing %d probes for target region: %s", len(regionResult.IpProbes), targetRegion)
		for _, probe := range regionResult.IpProbes {
			if probe.TargetIp == "" {
				log.Warningf("Warning: Received probing_report with empty TargetIp for source %s, target region %s. Skipping.", sourceIP, targetRegion)
				continue
			}
			//if probing_report.TcpDelay < 0 {
			//	log.Printf("Probe failed for TargetIp %s in region %s (from %s). Skipping metrics_storage or storing as error.", probing_report.TargetIp, targetRegion, sourceIP)
			//	continue
			//}
			dbEntry := &db_models.ProbeResult{
				SourceIP:     sourceIP,
				SourceRegion: sourceRegion,
				TargetIP:     probe.TargetIp,
				TargetRegion: targetRegion,
				TCPDelay:     probe.TcpDelay,
				ProbeTime:    probeTime,
			}
			err := db_models.InsertProbeResult(p.db, dbEntry)
			if err != nil {
				log.Errorf("Error inserting probing_report result into DB for [%s (%s) -> %s (%s)]: %v",
					sourceIP, sourceRegion, probe.TargetIp, targetRegion, err)
			}
		}
	}
	log.Infof("Finished processing probing_report results for source IP: %s", sourceIP)
	return nil
}

/*func (p *Processor) ProcessProbeResults(sourceIP string, results []*pb.RegionProbeResult, fileManager *metrics_storage.FileManager) error {
	probeTime := time.Now()
	sourceRegion, err := db_models.GetNodeRegion(p.db, sourceIP)
	if err != nil {
		log.Printf("IP: %v", err)
		sourceRegion = "unknown"
	}
	processedPairs := make(map[string]map[string]bool)
	regionAvgDelays := make(map[string]int64)
	p.extractProbeResults(results, sourceIP, sourceRegion, probeTime, processedPairs, regionAvgDelays)
	p.fillMissingProbeResults(sourceIP, sourceRegion, probeTime, processedPairs, regionAvgDelays, fileManager)
	log.Printf(" %s ，", sourceIP)
	return nil
}

func (p *Processor) extractProbeResults(
	regionProbeResults []*pb.RegionProbeResult,
	sourceIP string,
	sourceRegion string,
	probeTime time.Time,
	processedPairs map[string]map[string]bool,
	regionAvgDelays map[string]int64,
) {
	for _, regionResult := range regionProbeResults {
		targetRegion := regionResult.Region
		for _, probing_report := range regionResult.IpProbes {
			if probing_report.TargetIp == "normal_avg" {
				regionAvgDelays[targetRegion] = probing_report.TcpDelay
				err := db_models.InsertProbeResult(p.db, &db_models.ProbeResult{
					SourceIP:     sourceIP,
					SourceRegion: sourceRegion,
					TargetIP:     "normal_avg",
					TargetRegion: targetRegion,
					TCPDelay:     probing_report.TcpDelay,
					ProbeTime:    probeTime,
				})
				if err != nil {
					log.Printf(" [%s->%s]: %v",
						sourceRegion, targetRegion, err)
				}
			} else {
				targetIP := probing_report.TargetIp
				if _, exists := processedPairs[targetIP]; !exists {
					processedPairs[targetIP] = make(map[string]bool)
				}
				processedPairs[targetIP][targetRegion] = true
				err := db_models.InsertProbeResult(p.db, &db_models.ProbeResult{
					SourceIP:     sourceIP,
					SourceRegion: sourceRegion,
					TargetIP:     targetIP,
					TargetRegion: targetRegion,
					TCPDelay:     probing_report.TcpDelay,
					ProbeTime:    probeTime,
				})
				if err != nil {
					log.Printf(" [%s->%s, %s]: %v",
						sourceIP, targetIP, targetRegion, err)
				}
			}
		}
	}
}
func (p *Processor) fillMissingProbeResults(
	sourceIP string,
	sourceRegion string,
	probeTime time.Time,
	processedPairs map[string]map[string]bool,
	regionAvgDelays map[string]int64,
	fileManager *metrics_storage.FileManager,
) {
	probeTasks := fileManager.GetNodeTasks(sourceIP)
	if probeTasks == nil || len(probeTasks) == 0 {
		log.Printf(" %s ，", sourceIP)
		return
	}
	nodeList := fileManager.GetNodeList()
	if nodeList == nil || len(nodeList.Nodes) == 0 {
		log.Printf("，")
		return
	}
	ipToRegion := make(map[string]string)
	for _, node := range nodeList.Nodes {
		ipToRegion[node.Ip] = node.Region
	}
	for _, task := range probeTasks {
		targetIP := task.TargetIp
		targetRegion, exists := ipToRegion[targetIP]
		if !exists {
			log.Printf(": IP %s ，unknown", targetIP)
			targetRegion = "unknown"
		}
		if regionsProcessed, exists := processedPairs[targetIP]; exists && regionsProcessed[targetRegion] {
			continue
		}
		avgDelay, hasAvg := regionAvgDelays[targetRegion]
		if !hasAvg || avgDelay < 0 {
			log.Printf(":  %s ，IP %s ",
				targetRegion, targetIP)
			continue
		}
		err := db_models.InsertProbeResult(p.db, &db_models.ProbeResult{
			SourceIP:     sourceIP,
			SourceRegion: sourceRegion,
			TargetIP:     targetIP,
			TargetRegion: targetRegion,
			TCPDelay:     avgDelay,
			ProbeTime:    probeTime,
		})
		if err != nil {
			log.Printf(" [%s->%s, %s]: %v",
				sourceIP, targetIP, targetRegion, err)
		}
	}
}
*/
