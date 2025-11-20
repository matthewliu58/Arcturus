package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

// LoadConfig loads configuration from the specified TOML file
func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "structs.toml"
	}

	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("structs file not found: %s", configPath)
	}

	var cfg Config
	if _, err := toml.DecodeFile(configPath, &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode structs file: %w", err)
	}

	// Set defaults for optional fields
	if cfg.ProbeInterval == 0 {
		cfg.ProbeInterval = 5 // Default: 5 seconds
	}
	if cfg.ProbeTimeout == 0 {
		cfg.ProbeTimeout = 2000 // Default: 2000 milliseconds
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info" // Default: info
	}

	// Validate required fields
	if cfg.ControllerAddr == "" {
		return nil, fmt.Errorf("controller_addr is required in structs file")
	}

	return &cfg, nil
}

// SetupLogger configures the logger based on structs
func SetupLogger(cfg *Config) {
	level, err := log.ParseLevel(cfg.LogLevel)
	if err != nil {
		log.Warnf("Invalid log level '%s', using 'info'", cfg.LogLevel)
		level = log.InfoLevel
	}

	log.SetLevel(level)
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	// Get executable path and create logs directory
	exePath, _ := os.Executable()
	logDir := filepath.Join(filepath.Dir(exePath), "logs")
	os.MkdirAll(logDir, 0755)

	// Use lumberjack for log rotation (consistent with control plane and data plane)
	log.SetOutput(&lumberjack.Logger{
		Filename:   filepath.Join(logDir, "app.log"),
		MaxSize:    100,  // Max size in MB per log file
		MaxBackups: 7,    // Keep 7 recent backups
		MaxAge:     30,   // Keep logs for 30 days
		Compress:   true, // Compress old logs
	})
}
