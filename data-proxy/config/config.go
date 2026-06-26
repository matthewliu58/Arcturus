package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var (
	Config_     *Config
	TestPathMap = make(map[int]string)
	BatchNumMap = make(map[uint16]int)
)

type Config struct {
	Port                  string     `yaml:"port"`
	ControlHost           string     `yaml:"control_host"`
	TestRouting           []TestPath `yaml:"test_routing"`
	AggWorkerNum          int        `yaml:"agg_worker_num"`
	AggWorkerCount        int        `yaml:"agg_worker_count"`
	TcpWorkerCount        int        `yaml:"tcp_worker_count"`
	BackWorkerCount       int        `yaml:"back_worker_count"`
	TunnelConcurrentLimit int        `yaml:"tunnel_concurrent_limit"`
	Listeners             []Listener `yaml:"listeners"`
	RateLimit             RateLimit  `yaml:"rate_limit"`
	Aggregator            Aggregator `yaml:"aggregator"`
	Node                  NodeConfig `yaml:"node"`
}

type TestPath struct {
	Port int    `yaml:"port"`
	Path string `yaml:"path"`
}

type NodeConfig struct {
	Provider  string `yaml:"provider"`
	Continent string `yaml:"continent"`
	Country   string `yaml:"country"`
	City      string `yaml:"city"`
	IP        NodeIP `yaml:"ip"`
}

type NodeIP struct {
	Private string `yaml:"private"`
	Public  string `yaml:"public"`
}

type Listener struct {
	Proto    string `yaml:"proto"` // tcp / udp
	Port     int    `yaml:"port"`
	BatchNum int    `yaml:"batch_num"`
}

type RateLimit struct {
	QPS           int `yaml:"qps"`
	Burst         int `yaml:"burst"`
	CleanInterval int `yaml:"clean_interval"`
}

type Aggregator struct {
	BufferSize     int `yaml:"buffer_size"`
	BatchTimeoutMs int `yaml:"batch_timeout_ms"`
}

func ReadYamlConfig(logger *slog.Logger) (*Config, error) {

	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, "config.yaml")

	if _, err = os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("configuration file does not exist: %s", configPath)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration file: %w", err)
	}

	var config Config
	if err = yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("failed to parse yaml: %w", err)
	}

	// Load shared node config if module config doesn't have node
	if config.Node.Provider == "" {
		nodeConfigPath := findNodeConfig(exeDir)
		if nodeConfigPath == "" {
			if cwd, err := os.Getwd(); err == nil {
				nodeConfigPath = findNodeConfig(cwd)
			}
		}
		if nodeConfigPath == "" {
			logger.Error("Shared node config not found (config/node.yaml)")
		} else {
			logger.Info("Loading shared node config", slog.String("path", nodeConfigPath))
			content, err := os.ReadFile(nodeConfigPath)
			if err != nil {
				logger.Error("Failed to read shared node config", slog.String("path", nodeConfigPath), slog.Any("err", err))
			} else {
				var nodeConfig NodeConfig
				if err := yaml.Unmarshal(content, &nodeConfig); err != nil {
					logger.Error("Failed to parse shared node config", slog.String("path", nodeConfigPath), slog.Any("err", err))
				} else {
					config.Node = nodeConfig
					logger.Info("Loaded shared node configuration",
						slog.String("provider", nodeConfig.Provider),
						slog.String("continent", nodeConfig.Continent),
						slog.String("country", nodeConfig.Country),
						slog.String("city", nodeConfig.City),
						slog.String("public_ip", nodeConfig.IP.Public),
						slog.String("private_ip", nodeConfig.IP.Private))
				}
			}
		}
	}

	Config_ = &config

	testPathMap := make(map[int]string)
	for _, v := range config.TestRouting {
		testPathMap[v.Port] = v.Path
	}
	TestPathMap = testPathMap

	batchNumMap := make(map[uint16]int)
	for _, v := range config.Listeners {
		batchNumMap[uint16(v.Port)] = v.BatchNum
	}
	BatchNumMap = batchNumMap

	return &config, nil
}

// findNodeConfig walks up from startDir looking for config/node.yaml
func findNodeConfig(startDir string) string {
	for dir := startDir; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "config", "node.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
