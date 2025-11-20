package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// GetIP gets the public outbound IP address
// This function is copied from forwarding/metrics_processing/collector/metrics_collector.go
func GetIP() (string, error) {
	urls := []string{
		"http://icanhazip.com",
		"http://api.ipify.org",
		"http://ifconfig.me/ip",
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	var lastErr error
	for _, url := range urls {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = fmt.Errorf("failed to query %s: %w", url, err)
			continue
		}

		ipBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response from %s: %w", url, err)
			continue
		}

		ip := strings.TrimSpace(string(ipBytes))
		if ip != "" {
			return ip, nil
		}
	}

	return "", fmt.Errorf("all IP services failed: %w", lastErr)
}

// GetLocalIP gets the local IP address
// Prioritizes the manually configured IP in structs, otherwise auto-detects
func GetLocalIP(cfg *Config) (string, error) {
	// If LocalIP is specified in structs, use it directly
	if cfg.LocalIP != "" {
		log.Infof("Using manually configured local IP: %s", cfg.LocalIP)
		return cfg.LocalIP, nil
	}

	// Otherwise, auto-detect public IP
	log.Info("Local IP not specified in structs, attempting to auto-detect...")
	ip, err := GetIP()
	if err != nil {
		return "", fmt.Errorf("failed to auto-detect public IP: %w", err)
	}

	log.Infof("Auto-detected public IP: %s", ip)
	return ip, nil
}
