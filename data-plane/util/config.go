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

	return &config, nil
}
