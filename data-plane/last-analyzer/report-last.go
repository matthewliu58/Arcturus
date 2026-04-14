package last_analyzer

import (
	"bytes"
	"data-plane/util"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	report "data-plane/report-info"
)

const (
	lastReceiveURL = "http://127.0.0.1:7081/api/v1/last/receive"
)

type LastStats struct {
	DelayStats map[LastStatsKey]*LastStatsValue `json:"delay_stats"`
	IP         string                           `json:"ip"`
	ISP        string                           `json:"isp"`
	Continent  string                           `json:"continent"`
	Country    string                           `json:"country"`
	Province   string                           `json:"province"`
	City       string                           `json:"city"`
}

func SendLastStats(delayStats map[LastStatsKey]*LastStatsValue, pre string, logger *slog.Logger) error {

	c := util.Config_.Node

	stats := &LastStats{
		DelayStats: delayStats,
		IP:         c.IP.Public,
		ISP:        c.Provider,
		Continent:  c.Continent,
		Country:    c.Country,
		Province:   "",
		City:       c.City,
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

	resp, err := client.Post(lastReceiveURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Error("failed to send LastStats to control-plane", slog.String("pre", pre),
			slog.Any("error", err), slog.String("url", lastReceiveURL))
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
		return err
	}

	logger.Info("node statistics reported successfully", slog.String("pre", pre),
		slog.Int("delayStatsCount", len(delayStats)))
	return nil
}
