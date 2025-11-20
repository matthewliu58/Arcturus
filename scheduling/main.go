package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"scheduling/controller/client_probing_api"
	"scheduling/controller/last_mile_scheduling/bpr"
	"scheduling/controller/metrics_processing"
	"scheduling/controller/traefik_server"
	models "scheduling/db_models"
	"scheduling/middleware"
	"scheduling/structs"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

// log init
func init() {
	// Use WorkingDirectory for logs instead of executable directory
	logDir := "./logs"
	os.MkdirAll(logDir, 0755)

	// Configure log rotation with lumberjack
	fileLogger := &lumberjack.Logger{
		Filename:   logDir + "/scheduling.log",
		MaxSize:    100,  // MB
		MaxBackups: 7,    // Keep 7 old log files
		MaxAge:     30,   // Days
		Compress:   true, // Compress old log files
	}

	// Output to both file and stdout (for systemd)
	multiWriter := io.MultiWriter(os.Stdout, fileLogger)
	log.SetOutput(multiWriter)
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.SetLevel(log.InfoLevel)

	log.Infof("Logging initialized: file=%s/scheduling.log, stdout=enabled", logDir)
}

func main() {
	// Create a cancellable root context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure resource cleanup

	// Set up signal listening (only listen in main)
	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, os.Interrupt, syscall.SIGTERM)

	// Create a wait group to coordinate the shutdown of all services
	var wg sync.WaitGroup

	// Start graceful shutdown handler
	go func() {
		<-shutdownSignal
		log.Infof("Received shutdown signal. Initiating graceful shutdown...")
		cancel() // Cancel context to notify all services to stop
	}()

	// Prepare data - for registering domain names and IPs
	configPath := os.Getenv("ARCTURUS_CONFIG")
	if configPath == "" {
		// WorkingDirectory is set to /root/Arcturus/scheduling in systemd service
		configPath = "scheduling_config.toml"
	}

	log.Infof("Loading configuration from: %s", configPath)
	cfg, err := middleware.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration from %s: %v", configPath, err)
	}
	log.Infof("Configuration loaded successfully")

	log.Infof("Connecting to database: %s@%s", cfg.Database.Username, cfg.Database.DBName)
	db := middleware.ConnectToDB(cfg.Database)
	if db == nil {
		log.Fatalf("Failed to connect to database")
	}
	log.Infof("Database connected successfully")

	// Insert domain origin data
	if err := models.InsertDomainOrigins(db, cfg.DomainOrigins); err != nil {
		log.Infof("Error during domain_origin insertion: %v", err)
	} else {
		log.Infoln("Domain origins processing completed.")
	}

	// Insert node region data
	if err := models.InsertNodeRegions(db, cfg.NodeRegions); err != nil {
		log.Errorf("Error during node_region insertion: %v", err)
	} else {
		log.Infof("Node regions processing completed.")
	}
	if cfg.DomainConfigurations != nil {
		for _, dc := range cfg.DomainConfigurations {
			// For each domain configuration, save to both tables
			err := models.SaveOrUpdateDomainConfig(db, dc.DomainName, dc.TotalReqIncrement, dc.RegionReqIncrement, dc.RedistributionProportion)
			if err != nil {
				log.Errorf("Error saving domain configuration for '%s' from dispatch_task: %v", dc.DomainName, err)
			}
		}
	} else {
		log.Infof("No DomainConfigurations found in dispatch_task.")
		return
	}
	// Inject database connection into traefik_server package
	traefik_server.SetDatabase(db)
	log.Info("Database connection injected into traefik_server package")

	// Start metrics_processing server
	wg.Add(1)
	go func() {
		defer wg.Done()
		metrics_processing.StartServer(ctx, db)
		log.Infof("Heartbeats server stopped.")
	}()

	if cfg.BPRSchedulingTasks != nil && len(cfg.BPRSchedulingTasks) > 0 {
		//nodesCount, errDb := models.CountMetricsNodes(db)
		//if errDb != nil {
		//	log.Errorf("Failed to count metrics nodes: %v. BPR scheduling might not start.", errDb)
		//}

		//if nodesCount > 0 {
		for _, task := range cfg.BPRSchedulingTasks {
			wg.Add(1)
			go func(t structs.BPRSchedulingTaskConfig) {
				defer wg.Done()
				interval := time.Duration(t.IntervalSeconds) * time.Second
				if t.IntervalSeconds <= 0 {
					interval = 10 * time.Second
					log.Warningf("Warning: Invalid IntervalSeconds (%d) for region %s. "+
						"Using default: %v", t.IntervalSeconds, t.Region, interval)
				}
				log.Infof("[Main App] Starting BPR scheduling for Region=%s, Interval=%v",
					t.Region, interval)
				bpr.ScheduleBPRRuns(ctx, db, interval, t.Region)
				log.Infof("BPR scheduling for region %s stopped.", t.Region)
			}(task)
		}
		//} else {
		//	log.Warningf("[Main App] No metric nodes found. Skipping BPR scheduling based on dispatch_task.")
		//}
	} else {
		log.Warningf("[Main App] No BPRSchedulingTasks found in configuration. BPR scheduling will not start.")
	}

	// Start Traefik dispatch_task server
	wg.Add(1)
	go func() {
		defer wg.Done()
		traefik_server.RunServerWithContext(ctx)
		log.Infof("Traefik dispatch_task server stopped.")
	}()
	// Start Client Delay API server
	wg.Add(1)
	go func() {
		defer wg.Done()
		apiAddr := ":8081" // Client Delay Query API port
		client_probing_api.StartServer(ctx, db, apiAddr)
		log.Infof("Client Delay API server shutdown complete.")
	}()

	log.Infof("Application started. BPR scheduling and result polling active.")
	log.Infof("Press Ctrl+C to exit gracefully.")

	// Wait for context cancellation
	<-ctx.Done()
	log.Infof("Context canceled. Waiting for all services to stop...")

	// Give services some time to shut down gracefully
	shutdownTimeout := time.NewTimer(10 * time.Second)
	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Infof("All services stopped gracefully.")
	case <-shutdownTimeout.C:
		log.Infof("Shutdown timeout reached. Some services may not have stopped gracefully.")
	}

	// Clean up resources
	log.Infof("Closing database connection...")
	middleware.CloseDB()
	log.Infof("Database connection pool closed.")
	log.Infof("Shutdown complete. Exiting.")
}
