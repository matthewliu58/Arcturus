package server

import (
	"bytes"
	"data-proxy/config"
	"data-proxy/util"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

const (
	middleRoutingURL = "/api/v1/routing/middle"
)

type ControlPlaneRoutingResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data []util.PathInfo `json:"data"`
}

type EndPoint struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Provider  string `json:"provider"`
	Continent string `json:"continent"`
	Country   string `json:"country"`
	City      string `json:"city"`
}

type EndPoints struct {
	Source EndPoint `json:"source"`
	Dest   EndPoint `json:"dest"`
}

func GetRoutingFromControlPlane(port int, l *slog.Logger) *util.RoutingInfo {

	req := EndPoints{}
	req.Source.IP = config.Config_.Node.IP.Private
	req.Dest.Port = port

	l.Info("Requesting routing information from control plane", slog.Any("req", req))

	reqBody, err := json.Marshal(req)
	if err != nil {
		l.Error("Failed to marshal routing request", slog.Any("err", err))
		return &util.RoutingInfo{}
	}

	url := fmt.Sprintf("%s?ip=%s", config.Config_.ControlHost+middleRoutingURL, config.Config_.Node.IP.Public)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		l.Error("Failed to send routing request to control plane", slog.Any("err", err))
		return &util.RoutingInfo{}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		l.Error("Failed to read routing response", slog.Any("err", err))
		return &util.RoutingInfo{}
	}

	var routingResp ControlPlaneRoutingResponse
	if err = json.Unmarshal(respBody, &routingResp); err != nil {
		l.Error("Failed to unmarshal routing response", slog.Any("err", err))
		return &util.RoutingInfo{}
	}

	if routingResp.Code != 200 {
		l.Error("Control plane returned error", slog.String("msg", routingResp.Msg))
		return &util.RoutingInfo{}
	}

	return &util.RoutingInfo{
		Routing: routingResp.Data,
	}
}
