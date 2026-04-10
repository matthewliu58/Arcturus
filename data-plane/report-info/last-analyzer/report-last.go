package last_analyzer

import (
	"bytes"
	"data-plane/util"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	report_info "data-plane/report-info"
)

const (
	lastReceiveURL = "http://127.0.0.1:7081/api/v1/last/receive"
)

type LastStats struct {
	DelayStats map[LastStatsKey]*LastStatsValue `json:"delay_stats"` // 外部客户到本节点的时延统计 map
	IP         string                           `json:"ip"`          // 节点 IP
	ISP        string                           `json:"isp"`         // 节点 ISP
	Continent  string                           `json:"continent"`   // 节点所在大洲
	Country    string                           `json:"country"`     // 国家
	Province   string                           `json:"province"`    // 省份
	City       string                           `json:"city"`        // 城市
}

// SendLastStats 上报节点统计信息到 control-plane
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

	reqBody := report_info.ApiResponse{
		Code: 200,
		Msg:  pre + " success",
		Data: stats,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		logger.Error("序列化 LastStats 失败", slog.String("pre", pre), slog.Any("error", err))
		return err
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Post(lastReceiveURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Error("发送 LastStats 到 control-plane 失败", slog.String("pre", pre), slog.Any("error", err), slog.String("url", lastReceiveURL))
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("control-plane 返回非 200 状态码", slog.String("pre", pre), slog.Int("status", resp.StatusCode))
	}

	var respBody report_info.ApiResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		logger.Error("解析 control-plane 响应失败", slog.String("pre", pre), slog.Any("error", err))
		return err
	}

	if respBody.Code != 200 {
		logger.Error("control-plane 处理失败", slog.String("pre", pre), slog.Int("code", respBody.Code), slog.String("msg", respBody.Msg))
		return err
	}

	logger.Info("节点统计信息上报成功", slog.String("pre", pre), slog.Int("delayStatsCount", len(delayStats)))
	return nil
}
