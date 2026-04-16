package api

import (
	"control-plane/aggregator"
	model "control-plane/receive-info"
	"control-plane/sync/etcd_client"
	"control-plane/util"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	probeTargets = "probe-targets.json"
)

var (
	SourceTargetMap map[int]SourceTarget
)

type NodeProbeAPIHandler struct {
	etcdClient *clientv3.Client
	logger     *slog.Logger
}

type SourceTarget struct {
	ServerPort int    `json:"server_port"`
	Provider   string `json:"provider"`
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	Region     string `json:"region"`
	ID         string `json:"id"`
}

func LoadSourceTargetsFromExeDir() (map[int]SourceTarget, error) {

	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("get executable path failed: %w", err)
	}

	exeDir := filepath.Dir(exePath)

	targetFile := filepath.Join(exeDir, probeTargets)

	data, err := os.ReadFile(targetFile)
	if err != nil {
		return nil, fmt.Errorf("read cloud storage targets file failed (%s): %w", targetFile, err)
	}

	var targets []SourceTarget
	if err = json.Unmarshal(data, &targets); err != nil {
		return nil, fmt.Errorf("unmarshal cloud storage targets failed: %w", err)
	}

	sourceTargetMap := make(map[int]SourceTarget)
	for _, v := range targets {
		sourceTargetMap[v.ServerPort] = v
	}

	return sourceTargetMap, nil
}

func NewNodeProbeAPIHandler(cli *clientv3.Client, logger *slog.Logger) *NodeProbeAPIHandler {
	return &NodeProbeAPIHandler{
		etcdClient: cli,
		logger:     logger,
	}
}

func (h *NodeProbeAPIHandler) GetProbeTasks(c *gin.Context) {
	resp := model.ApiResponse{
		Code: 500,
		Msg:  "Internal server error",
		Data: nil,
	}

	pre := c.Query("ip")
	if pre == "" {
		pre = util.GenerateRandomLetters(5)
	}

	nodeMap, err := etcd_client.GetPrefixAll(h.etcdClient, "/routing-middle/", pre, h.logger)
	if err != nil {
		resp.Code = 500
		resp.Msg = "Failed to get node information: " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg, slog.String("pre", pre))
		return
	}

	var tasks []model.ProbeTask
	ip_ := util.Config_.Node.IP.Public
	for k, nodeJson := range nodeMap {
		var telemetry aggregator.NodeTelemetry
		if err = json.Unmarshal([]byte(nodeJson), &telemetry); err != nil {
			h.logger.Warn("Failed to parse node JSON, skipping", slog.String("pre", pre),
				slog.String("ip", k), slog.Any("error", err))
			continue
		}

		if telemetry.PublicIP == ip_ {
			continue
		}

		tasks = append(tasks, model.ProbeTask{
			TargetType: "node",
			Provider:   telemetry.Provider,
			IP:         telemetry.PublicIP,
			Port:       7083,
			Region:     telemetry.Continent,
		})
	}

	for _, v := range SourceTargetMap {
		tasks = append(tasks, model.ProbeTask{
			TargetType: "source",
			Provider:   v.Provider,
			IP:         v.IP,
			Port:       v.Port,
			Region:     v.Region,
			ID:         v.ID,
		})
	}

	resp.Code = 200
	resp.Msg = "Successfully obtained node probe tasks"
	resp.Data = tasks
	c.JSON(http.StatusOK, resp)
}

func InitNodeProbeRouter(router *gin.Engine, cli *clientv3.Client, logger *slog.Logger) *gin.Engine {
	r := router
	apiV1 := r.Group("/api/v1")
	{
		probeGroup := apiV1.Group("/probe")
		{
			handler := NewNodeProbeAPIHandler(cli, logger)
			probeGroup.GET("/tasks", handler.GetProbeTasks)
		}
	}
	return r
}
