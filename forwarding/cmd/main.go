package main

import (
	"context"
	"fmt"
	"forwarding/forwarding"
	"forwarding/metrics_processing"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Config struct to hold configuration from toml file
type ForwardingConfig struct {
	Metrics MetricsConfig `toml:"metrics_processing"`
}

type MetricsConfig struct {
	ServerAddr string `toml:"server_addr"`
}

func loadConfig(path string) (*ForwardingConfig, error) {
	var config ForwardingConfig
	if _, err := toml.DecodeFile(path, &config); err != nil {
		return nil, fmt.Errorf("failed to load task_dispatching file %s: %w", path, err)
	}
	if config.Metrics.ServerAddr == "" {
		log.Warningf("Metrics ServerAddr not specified in task_dispatching, using default or handling error as needed.")
		config.Metrics.ServerAddr = "127.0.0.1:8080"
	}
	return &config, nil
}

// log init
func init() {
	// Use WorkingDirectory for logs instead of executable directory
	logDir := "./logs"
	os.MkdirAll(logDir, 0755)

	// Configure log rotation with lumberjack
	fileLogger := &lumberjack.Logger{
		Filename:   logDir + "/forwarding.log",
		MaxSize:    100,  // MB
		MaxBackups: 7,    // Keep 7 old log files
		MaxAge:     30,   // Days
		Compress:   true, // Compress old log files
	}

	// Output to both file and stdout (for systemd)
	multiWriter := io.MultiWriter(os.Stdout, fileLogger)
	log.SetOutput(multiWriter)

	// Set formatter for logrus
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	// Set log level to Info
	log.SetLevel(log.InfoLevel)

	log.Infof("Logging initialized: file=%s/forwarding.log, stdout=enabled", logDir)
}

func main() {

	// task_dispatching file
	cfg, err := loadConfig("forwarding_config.toml")
	if err != nil {
		log.Fatalf("loading configuration failed, err:%v", err)
		return
	}

	// probing_report logic
	port := fmt.Sprintf(":%d", 50051)
	listener, err := net.Listen("tcp", port)
	defer listener.Close()
	if err != nil {
		log.Fatalf("listening tcp failed, port:%v, err:%v", port, err)
		return
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Fatalf("probing_report accept failed, err:%v", err)
			}
			remoteAddr := conn.RemoteAddr().String()
			log.Infof("probing_report accepte success, remote addr:%v", remoteAddr)
			conn.Close()
		}
	}()
	log.Infof("listening tcp success, port:%v", port)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	//initiate data plane
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Preload domain PathManagers for routing
	//log.Infof("Preloading domain PathManagers...")
	//routing.PreloadDomains()
	//log.Infof("Domain PathManagers preloaded")

	go metrics_processing.StartDataPlane(ctx, cfg.Metrics.ServerAddr)
	go forwarding.AccessProxyfunc()
	go forwarding.RelayProxyfunc()

	log.Infof("forwarding init success")
	<-signalChan
	log.Infof("received signal, shutting down")
	cancel()
	time.Sleep(1 * time.Second)
}
