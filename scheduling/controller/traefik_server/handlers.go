package traefik_server

import (
	"database/sql"
	"encoding/json"
	"net"
	"strings"

	"net/http"

	log "github.com/sirupsen/logrus"

	"scheduling/db_models"
)

// getClientIP extracts the real client IP from HTTP request
// It checks X-Forwarded-For and X-Real-IP headers before falling back to RemoteAddr
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (if behind proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Use RemoteAddr directly
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // Return as-is if parsing fails
	}
	return ip
}

// traefikConfigHandler handles configuration requests from Traefik
// It identifies the Traefik node's region based on its source IP and returns region-specific configuration
func traefikConfigHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Extract Traefik node's source IP
	sourceIP := getClientIP(r)
	log.Infof("Received Traefik config request from IP: %s", sourceIP)

	// 2. Query database to get the region for this IP
	region, err := db_models.GetNodeRegion(db, sourceIP)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warnf("IP %s not found in node_region table, returning empty config", sourceIP)
		} else {
			log.Errorf("Failed to get region for Traefik IP %s: %v, returning empty config", sourceIP, err)
		}
		// Return empty configuration on error
		emptyConfig := &TraefikDynamicConfiguration{
			HTTP: &HTTPConfiguration{
				Routers:     make(map[string]Router),
				Middlewares: make(map[string]Middleware),
				Services:    make(map[string]Service),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(emptyConfig)
		return
	}

	log.Infof("Traefik IP %s belongs to region: %s", sourceIP, region)

	// 3. Generate region-specific configuration
	configToServe := generateRegionConfig(region)

	if configToServe == nil {
		log.Errorf("Failed to generate configuration for region %s", region)
		emptyConfig := &TraefikDynamicConfiguration{
			HTTP: &HTTPConfiguration{
				Routers:     make(map[string]Router),
				Middlewares: make(map[string]Middleware),
				Services:    make(map[string]Service),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(emptyConfig)
		return
	}

	// 4. Return configuration
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(configToServe); err != nil {
		log.Errorf("Error encoding configuration to JSON: %v", err)
	}

	log.Infof("Successfully returned Traefik config for region %s to IP %s", region, sourceIP)
}
