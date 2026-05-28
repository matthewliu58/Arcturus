package edge_domain

import (
	"bytes"
	"data-plane/util"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	report "data-plane/report-info"
)

const (
	lastReceiveURL = "/api/v1/last/receive"
)

type LastTelemetry struct {
	LastsCongestion map[string]*LastCongestion `json:"lasts_congestion"`
	IP              string                     `json:"ip"`
	Continent       string                     `json:"continent"`
	Country         string                     `json:"country"`
	City            string                     `json:"city"`
}

func SendLastTelemetry(delayStats map[string]*LastCongestion, pre string, logger *slog.Logger) error {

	c := util.Config_.Node

	stats := &LastTelemetry{
		LastsCongestion: delayStats,
		IP:              c.IP.Public,
		Continent:       c.Continent,
		Country:         c.Country,
		City:            c.City,
	}

	reqBody := report.ApiResponse{
		Code: 200,
		Msg:  "success",
		Data: stats,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		logger.Error("failed to serialize LastStats", slog.String("pre", pre), slog.Any("error", err))
		return err
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	url := fmt.Sprintf("%s?ip=%s", util.Config_.ControlHost+lastReceiveURL, util.Config_.Node.IP.Public)
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Error("failed to send LastStats to control-plane", slog.String("pre", pre),
			slog.Any("error", err), slog.String("url", url))
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("control-plane returned non-200 status code", slog.String("pre", pre),
			slog.Int("status", resp.StatusCode))
	}

	var respBody report.ApiResponse
	if err = json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		logger.Error("failed to parse control-plane response", slog.String("pre", pre), slog.Any("error", err))
		return err
	}

	if respBody.Code != 200 {
		logger.Error("control-plane processing failed", slog.String("pre", pre),
			slog.Int("code", respBody.Code), slog.String("msg", respBody.Msg))
		return fmt.Errorf("control-plane processing failed: code=%d, msg=%s", respBody.Code, respBody.Msg)
	}

	logger.Info("node statistics reported successfully", slog.String("pre", pre),
		slog.Int("delayStatsCount", len(delayStats)))
	return nil
}
