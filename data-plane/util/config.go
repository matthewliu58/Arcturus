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
	ControlHost string     `yaml:"control_host"`
	AccessLog   string     `yaml:"access_log"`
	Node        NodeConfig `yaml:"node"`
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
		nodeConfigPath := filepath.Join(exeDir, "..", "..", "config", "node.yaml")
		if content, err := os.ReadFile(nodeConfigPath); err == nil {
			var nodeConfig NodeConfig
			if err := yaml.Unmarshal(content, &nodeConfig); err == nil {
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

	return &config, nil
}
