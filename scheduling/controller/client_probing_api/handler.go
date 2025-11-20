package client_probing_api

import (
	"database/sql"
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"net"
	"net/http"
	"scheduling/db_models"
	"strings"
)

// Handler holds dependencies for the API handlers.
type Handler struct {
	db  *sql.DB
	dao *ClientDelayDAO
}

// NewHandler creates a new Handler with its dependencies.
func NewHandler(db *sql.DB) *Handler {
	return &Handler{
		db:  db,
		dao: NewClientDelayDAO(db),
	}
}

// RegisterRoutes sets up the routing for the API.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/region-nodes", h.handleGetRegionNodes)
	mux.HandleFunc("POST /api/v1/report-delays", h.handleReportDelays)
}

// handleGetRegionNodes provides a list of nodes based on the probing_client's region.
func (h *Handler) handleGetRegionNodes(w http.ResponseWriter, r *http.Request) {
	sourceIP := extractSourceIP(r)

	region, err := db_models.GetNodeRegion(h.db, sourceIP)
	if err != nil || region == "unknown" {
		log.WithField("ip", sourceIP).Warn("Region not found, returning empty list.")
		respondJSON(w, http.StatusOK, []NodeInfo{})
		return
	}

	ips, err := db_models.GetRegionIPs(h.db, region)
	if err != nil {
		log.WithField("region", region).Errorf("Failed to get IPs for region: %v", err)
		respondError(w, http.StatusInternalServerError, "Failed to get nodes for the region")
		return
	}

	nodes := make([]NodeInfo, 0, len(ips))
	for _, ip := range ips {
		nodes = append(nodes, NodeInfo{IP: ip, Port: 50051}) // Port should be configurable
	}
	respondJSON(w, http.StatusOK, nodes)
}

// handleReportDelays receives and stores probing_client latency measurements.
func (h *Handler) handleReportDelays(w http.ResponseWriter, r *http.Request) {
	var payload DelayReportPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}
	defer r.Body.Close()

	if payload.SourceIP == "" || len(payload.Delays) == 0 {
		respondError(w, http.StatusBadRequest, "Invalid data: sourceIP and delays are required")
		return
	}

	sourceRegion, err := db_models.GetNodeRegion(h.db, payload.SourceIP)
	if err != nil || sourceRegion == "unknown" {
		sourceRegion = "unknown"
	}

	err = h.dao.BatchInsertClientDelays(payload.SourceIP, sourceRegion, payload.Delays)
	if err != nil {
		log.Errorf("DAO failed to save delay data for source %s: %v", payload.SourceIP, err)
		respondError(w, http.StatusInternalServerError, "Failed to save data")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Helper Functions ---

func extractSourceIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

func respondJSON(w http.ResponseWriter, statusCode int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	if payload != nil {
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			log.Errorf("Failed to encode JSON response: %v", err)
		}
	}
}

func respondError(w http.ResponseWriter, statusCode int, message string) {
	respondJSON(w, statusCode, map[string]string{"error": message})
}
