package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

func main() {
	// Parse command line flags
	configPath := flag.String("structs", "structs.toml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load structs: %v", err)
	}

	// Setup logger
	SetupLogger(cfg)

	log.Info("========================================")
	log.Info("Traefik Client Delay Probe Module")
	log.Info("========================================")
	log.Infof("Controller: %s", cfg.ControllerAddr)
	log.Infof("Probe Interval: %d seconds", cfg.ProbeInterval)
	log.Infof("Probe Timeout: %d milliseconds", cfg.ProbeTimeout)
	log.Infof("Log Level: %s", cfg.LogLevel)

	// Get local IP
	localIP, err := GetLocalIP(cfg)
	if err != nil {
		log.Fatalf("Failed to get local IP: %v", err)
	}
	log.Infof("Local IP: %s", localIP)

	// Create API probing_client
	apiClient := NewAPIClient(cfg.ControllerAddr)

	// Fetch region nodes on startup
	log.Info("Fetching initial node list from controller...")
	nodes, err := apiClient.GetRegionNodes()
	if err != nil {
		log.Fatalf("Failed to fetch region nodes: %v", err)
	}

	if len(nodes) == 0 {
		log.Warn("No nodes found in region, will retry periodically...")
	} else {
		//log.Infof("Found %d nodes in region", len(nodes))
		//for i, node := range nodes {
		//	log.Infof("  Node %d: %s:%d", i+1, node.IP, node.Port)
		//}
	}

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Infof("Received shutdown signal: %v", sig)
		log.Info("Gracefully shutting down...")
		cancel()
	}()

	// Start probing_report loop
	log.Info("Starting probing_report loop...")
	runProbeLoop(ctx, apiClient, localIP, nodes, cfg)

	log.Info("Traefik Client Delay Probe module stopped.")
}

// runProbeLoop runs the main probing_report loop
func runProbeLoop(ctx context.Context, apiClient *APIClient, localIP string, initialNodes []NodeInfo, cfg *Config) {
	ticker := time.NewTicker(time.Duration(cfg.ProbeInterval) * time.Second)
	defer ticker.Stop()

	nodes := initialNodes
	lastNodeFetch := time.Now()
	nodeFetchInterval := 60 * time.Second // Refresh node list every 60 seconds

	for {
		select {
		case <-ctx.Done():
			log.Info("Context canceled, stopping probing_report loop")
			return

		case <-ticker.C:
			// Periodically refresh node list
			if time.Since(lastNodeFetch) > nodeFetchInterval {
				log.Debug("Refreshing node list from controller...")
				freshNodes, err := apiClient.GetRegionNodes()
				if err != nil {
					log.Warnf("Failed to refresh node list: %v, using cached list", err)
				} else {
					nodes = freshNodes
					lastNodeFetch = time.Now()
					log.Infof("Node list refreshed: %d nodes", len(nodes))
				}
			}

			// Skip probing_report if no nodes
			if len(nodes) == 0 {
				log.Warn("No nodes to probing_report, skipping this cycle")
				continue
			}

			// Perform probes
			log.Debug("Starting probing_report cycle...")
			results := ProbeAllNodes(nodes, cfg.ProbeTimeout)

			// Filter successful results
			successfulDelays := FilterSuccessful(results)

			// Report to controller (only if we have successful results)
			if len(successfulDelays) > 0 {
				payload := DelayReportPayload{
					SourceIP: localIP,
					Delays:   successfulDelays,
				}

				if err := apiClient.ReportDelays(payload); err != nil {
					log.Errorf("Failed to report delays: %v", err)
				} else {
					log.Infof("Probe cycle completed: %d/%d successful",
						len(successfulDelays), len(results))
				}
			} else {
				log.Warn("No successful probes in this cycle, skipping report")
			}
		}
	}
}
