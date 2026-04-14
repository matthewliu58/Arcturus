package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var (
	Config_ *Config
)

type Config struct {
	Port        string     `yaml:"port"`
	ControlHost string     `yaml:"control_host"`
	Listeners   []Listener `yaml:"listeners"`
	RateLimit   RateLimit  `yaml:"rate_limit"`
	Aggregator  Aggregator `yaml:"aggregator"`
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

type Listener struct {
	Proto string `yaml:"proto"` // tcp / udp
	Port  int    `yaml:"port"`
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

	return &config, nil
}
