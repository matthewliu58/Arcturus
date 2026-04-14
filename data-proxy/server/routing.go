package server

import (
	"bytes"
	"data-proxy/config"
	"data-proxy/util"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

// ControlPlaneRoutingResponse control plane routing response structure
type ControlPlaneRoutingResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data []util.PathInfo `json:"data"`
}

// EndPoint represents a network endpoint
type EndPoint struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Provider  string `json:"provider"`
	Continent string `json:"continent"`
	Country   string `json:"country"`
	City      string `json:"city"`
}

// EndPoints represents source and destination endpoints
type EndPoints struct {
	Source EndPoint `json:"source"`
	Dest   EndPoint `json:"dest"`
}

// GetRoutingFromControlPlane get routing information from control plane
func GetRoutingFromControlPlane(port int, l *slog.Logger) *util.RoutingInfo {
	// Construct request body
	req := EndPoints{}
	req.Source.IP = config.Config_.Node.IP.Private
	req.Dest.Port = port

	l.Info("Requesting routing information from control plane", slog.Any("req", req))

	// Serialize request body
	reqBody, err := json.Marshal(req)
	if err != nil {
		l.Error("Failed to marshal routing request", slog.Any("err", err))
		return &util.RoutingInfo{}
	}

	// Send HTTP request to control plane
	controlHost := config.Config_.ControlHost
	resp, err := http.Post(controlHost+"/api/v1/routing/middle", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		l.Error("Failed to send routing request to control plane", slog.Any("err", err))
		return &util.RoutingInfo{}
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		l.Error("Failed to read routing response", slog.Any("err", err))
		return &util.RoutingInfo{}
	}

	// Deserialize response body
	var routingResp ControlPlaneRoutingResponse
	if err := json.Unmarshal(respBody, &routingResp); err != nil {
		l.Error("Failed to unmarshal routing response", slog.Any("err", err))
		return &util.RoutingInfo{}
	}

	// Check response status
	if routingResp.Code != 200 {
		l.Error("Control plane returned error", slog.String("msg", routingResp.Msg))
		return &util.RoutingInfo{}
	}

	// Construct routing information
	return &util.RoutingInfo{
		Routing: routingResp.Data,
	}
}
