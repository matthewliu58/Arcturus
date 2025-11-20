package metrics_processing

import (
	"context"
	"encoding/json"
	t "forwarding/common"
	collector2 "forwarding/metrics_processing/collector"
	"forwarding/metrics_processing/probing_client"
	"forwarding/metrics_processing/probing_report"
	"forwarding/metrics_processing/protocol"
	"forwarding/metrics_processing/storage"
	to "forwarding/metrics_processing/topology"
	"forwarding/middle_mile_scheduling/common"
	"forwarding/routing"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

var (
	ReportInterval        = 10 * time.Second      // Topology sync interval
	RoutingInterval       = 15 * time.Second      // Path calculation interval
	DomainRefreshInterval = 30 * time.Second      // Domain refresh interval (check for new/deleted domains)
	DataDir               = "../../agent_storage" //

	bFlag atomic.Bool
)

func StartDataPlane(ctx context.Context, serverAddr string) {

	log.Infof("metrics_processing processing starting, server addr: %v\n", serverAddr)

	//init store dir
	absDataDir, err := filepath.Abs(DataDir)
	if err != nil {
		log.Fatalf("cant find data dir, err: %v\n", err)
		return
	}
	if err := os.MkdirAll(absDataDir, 0755); err != nil {
		log.Fatalf("cant change data dir perm, err: %v\n", err)
		return
	}
	fileManager, err := storage.NewFileManager(absDataDir)
	if err != nil {
		log.Fatalf("cant new file manager, dir: %v, err: %v\n", absDataDir, err)
		return
	}

	grpcClient, err := probing_client.NewGrpcClient(serverAddr, fileManager)
	defer grpcClient.Close()
	if err != nil {
		log.Fatalf("cant new grpc probing_client, serverAddr: %v, err: %v\n", serverAddr, err)
		return
	}

	info, err := collector2.CollectSystemInfo()
	if err != nil {
		log.Fatalf("collect system info failed, err: %v\n", err)
		return
	}

	//init forwarding and sync with controller
	if !fileManager.IsInitialized() {
		metrics := collector2.ConvertToProtoMetrics(info)
		initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer initCancel()
		if err := grpcClient.InitDataPlane(initCtx, metrics); err != nil {
			log.Fatalf("init grpc probing_client fialed, err: %v\n", err)
		}
	}

	topologyTicker := time.NewTicker(ReportInterval)
	routingTicker := time.NewTicker(RoutingInterval)
	domainRefreshTicker := time.NewTicker(DomainRefreshInterval)
	defer topologyTicker.Stop()
	defer routingTicker.Stop()
	defer domainRefreshTicker.Stop()

	var network common.Network
	var ipToIndex map[string]int
	var indexToIP map[int]string

	// Track current domains to detect changes
	var currentDomains map[string]string = make(map[string]string) // domain -> IP

	//time.Sleep(10 * time.Second)

	// Initialize domains on first run
	//func() {
	//	mappings := routing.GetAllDomainMapIP()
	//	for _, mapping := range mappings {
	//		currentDomains[mapping.Domain] = mapping.Ip
	//		// Create PathManager for each domain at startup
	//		pm := routing.GetInstance(mapping.Domain)
	//		if pm != nil {
	//			log.Infof("PathManager created for domain: %s -> %s", mapping.Domain, mapping.Ip)
	//		} else {
	//			log.Warningf("Failed to create PathManager for domain: %s", mapping.Domain)
	//		}
	//	}
	//	if len(currentDomains) > 0 {
	//		log.Infof("Initialized %d domains on startup", len(currentDomains))
	//	}
	//}()

	for {
		select {
		case <-domainRefreshTicker.C:
			// Domain refresh phase (every 30 seconds)
			log.Infof("Domain refresh check started")
			newMappings := routing.GetAllDomainMapIP()
			newDomains := make(map[string]string)
			for _, mapping := range newMappings {
				newDomains[mapping.Domain] = mapping.Ip
			}

			// Check for added domains
			for domain, ip := range newDomains {
				if _, exists := currentDomains[domain]; !exists {
					// New domain added
					log.Infof("New domain detected: %s -> %s", domain, ip)
					pm := routing.GetInstance(domain)
					if pm != nil {
						log.Infof("PathManager created for new domain: %s", domain)
					} else {
						log.Warningf("Failed to create PathManager for new domain: %s", domain)
					}
					currentDomains[domain] = ip
				}
			}

			// Check for deleted domains
			for domain := range currentDomains {
				if _, exists := newDomains[domain]; !exists {
					// Domain deleted
					log.Infof("Domain deleted: %s", domain)
					routing.RemoveInstance(domain)
					delete(currentDomains, domain)
				}
			}

			// Log current domain count
			if len(newDomains) > 0 {
				log.Debugf("Current domains: %d", len(newDomains))
			}

		case <-topologyTicker.C:
			log.Infof("topologyTicker 1")
			// Topology sync phase (every 5 seconds)
			info_, err := collector2.CollectSystemInfo()
			if err != nil {
				log.Warningf("collect sys info failed, err: %v", err)
				continue
			}
			metrics := collector2.ConvertToProtoMetrics(info_)
			regionProbeResults, err := probing_report.CollectRegionProbeResults(fileManager, info_.IP)
			if err != nil {
				log.Warningf("collect regional probing_report failed, err :%v", err)
				regionProbeResults = []*protocol.RegionProbeResult{}
			}

			log.Infof("topologyTicker 2")
			syncCtx, syncCancel := context.WithTimeout(context.Background(), 5*time.Second)
			syncResp, err := grpcClient.SyncMetrics(syncCtx, metrics, regionProbeResults)
			syncCancel()
			if err != nil {
				log.Warningf("grpc sync metrics_processing failed, err:%v", err)
				continue
			}
			log.Infof("topologyTicker 3")
			//mappings := fileManager.GetDomainIPMappings()
			mappings := routing.GetAllDomainMapIP()
			log.Infof("GetAllDomainMapIP111,%v,%v,%v", mappings, len(mappings), fileManager.GetDomainIPMappings())
			//mappings := fileManager.GetDomainIPMappings()
			if bFlag.Load() == false && len(mappings) > 0 {
				log.Infof("GetAllDomainMapIP,%v,%v", mappings, len(mappings))
				for _, mapping := range mappings {
					currentDomains[mapping.Domain] = mapping.Ip
					// Create PathManager for each domain at startup
					pm := routing.GetInstance(mapping.Domain)
					if pm != nil {
						log.Infof("PathManager created for domain: %s -> %s", mapping.Domain, mapping.Ip)
					} else {
						log.Warningf("Failed to create PathManager for domain: %s", mapping.Domain)
					}
				}
				if len(currentDomains) > 0 {
					log.Infof("Initialized %d domains on startup", len(currentDomains))
				}
				bFlag.Store(true)
			}

			if bFlag.Load() == false {
				log.Warningf("domainList is nil, cannot process RegionAssessments.")
				continue
			}

			//domainList := fileManager.GetDomainIPMappings()
			//if len(domainList) <= 0 {
			//	log.Warningf("domainList is nil, cannot process RegionAssessments.")
			//	continue
			//}

			//log.Infof("domainList:%s, nodeList:%s, probeTasks:%s",
			//	fileManager.GetDomainIPMappings(), fileManager.GetNodeList(), fileManager.GetDomainIPMappings())

			var baseTopology *t.TopologyGraph
			if len(syncResp.RegionAssessments) > 0 {
				nodeList := fileManager.GetNodeList()
				if nodeList == nil {
					log.Warningf("nodeList is nil, cannot process RegionAssessments.")
					continue
				}
				//get the base topology
				baseTopology, err = to.ProcessRegionAssessments(syncResp.RegionAssessments, nodeList)
				if err != nil {
					log.Warningf("error processing RegionAssessments,err:%v", err)
					continue
				}
			}

			// Only process topology if baseTopology is not nil
			if baseTopology != nil {
				currentNodeIP := info_.IP
				localProbeResults := regionProbeResults
				for _, regionResult := range localProbeResults {
					if len(regionResult.IpProbes) <= 0 {
						continue
					}
					for _, probeEntry := range regionResult.IpProbes {
						baseTopology.AddLink(currentNodeIP, probeEntry.TargetIp, float32(probeEntry.TcpDelay))
						log.Infof("Added/Updated local direct link to topology: %s -> %s, delay: %dms",
							currentNodeIP, probeEntry.TargetIp, probeEntry.TcpDelay)
					}
				}
				RegionAssessmentsJSON, _ := json.MarshalIndent(syncResp.RegionAssessments, "", "  ")
				localProbeResultsJSON, _ := json.MarshalIndent(localProbeResults, "", "  ")
				log.Infof("SetTopology stage 1, RegionAssessmentsJSON:%s, localProbeResultsJSON:%s",
					RegionAssessmentsJSON, localProbeResultsJSON)
				baseTopologyJSON, _ := json.MarshalIndent(baseTopology, "", "  ")
				log.Infof("SetTopology satge 2, baseTopologyJSON:%s", baseTopologyJSON)

				// Update topology
				topologyManager := t.GetInstance()
				topologyManager.SetTopology(baseTopology)
				network, ipToIndex, indexToIP, _ = topologyManager.GetNetwork()
				log.Infof("Topology updated")
			} else {
				log.Warningf("baseTopology is nil, skipping topology update and local link processing")
			}

			//case <-routingTicker.C:
			// Path calculation phase (every 10 seconds)
			if len(network.Nodes) > 0 {
				// Ensure PathManagers exist for all domains before calculating paths
				allMappings := routing.GetAllDomainMapIP()
				if len(allMappings) > 0 {
					for _, mapping := range allMappings {
						if _, exists := currentDomains[mapping.Domain]; !exists {
							// Domain found but not yet initialized
							log.Infof("Initializing PathManager for domain: %s -> %s", mapping.Domain, mapping.Ip)
							pm := routing.GetInstance(mapping.Domain)
							if pm != nil {
								log.Infof("PathManager created for domain: %s", mapping.Domain)
								currentDomains[mapping.Domain] = mapping.Ip
							} else {
								log.Warningf("Failed to create PathManager for domain: %s", mapping.Domain)
							}
						}
					}
				}

				log.Infof("Calculating paths for all domains")
				routing.CalculatePathsForAllDomains(network, ipToIndex, indexToIP)
			}

		case <-ctx.Done():
			return
		}
	}
}
