package util

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var Config_ *Config

type Config struct {
	//Name       string   `yaml:"name"`
	IpLib      string     `yaml:"ip_lib"`
	ServerList []string   `yaml:"server_list"`
	ServerIP   string     `yaml:"server_ip"`
	DataDir    string     `yaml:"data_dir"`
	Node       NodeConfig `yaml:"node"`
}

type NodeConfig struct {
	Provider  string `yaml:"provider"` // gcp | aws | azure | vultr | digitalocean | onprem
	Continent string `yaml:"continent"`
	Country   string `yaml:"country"`
	City      string `yaml:"city"`
	IP        NodeIP `yaml:"ip"`
}

type NodeIP struct {
	Private string `yaml:"private"`
	Public  string `yaml:"public"`
}

func ReadYamlConfig(logger *slog.Logger) (*Config, error) {

	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, "config.yaml")

	if _, err = os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("Configuration file does not exist: %s ", configPath)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration file: %w", err)
	} else {
		logger.Info("Successfully read configuration file", slog.String("path", configPath))
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
