package api

import (
	"control-plane/aggregator"
	model "control-plane/receive-info"
	"control-plane/sync/etcd_client"
	"control-plane/util"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	clientv3 "go.etcd.io/etcd/client/v3"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

const (
	probeTargets = "probe-targets.json"
	CloudStorage = "cloud_storage"
	Node         = "node"
)

var (
	CloudStorageMap map[int]CloudStorageTarget
)

type NodeProbeAPIHandler struct {
	etcdClient *clientv3.Client
	logger     *slog.Logger
}

type CloudStorageTarget struct {
	ServerPort int    `json:"server_port"`
	Provider   string `json:"provider"`
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	Region     string `json:"region"`
	ID         string `json:"id"`
}

func LoadCloudStorageTargetsFromExeDir() (map[int]CloudStorageTarget, error) {

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

	var targets []CloudStorageTarget
	if err = json.Unmarshal(data, &targets); err != nil {
		return nil, fmt.Errorf("unmarshal cloud storage targets failed: %w", err)
	}

	cloudStorageMap := make(map[int]CloudStorageTarget)
	for _, v := range targets {
		cloudStorageMap[v.ServerPort] = v
	}

	return cloudStorageMap, nil
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
		Msg:  "服务端内部错误",
		Data: nil,
	}

	pre := util.GenerateRandomLetters(5)

	nodeMap, err := etcd_client.GetPrefixAll(h.etcdClient, "/routing/", pre, h.logger)
	if err != nil {
		resp.Code = 500
		resp.Msg = "获取节点信息失败：" + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	var tasks []model.ProbeTask
	ip_ := util.Config_.Node.IP.Public
	for k, nodeJson := range nodeMap {
		var telemetry aggregator.Telemetry
		if err := json.Unmarshal([]byte(nodeJson), &telemetry); err != nil {
			h.logger.Warn("解析节点JSON失败，跳过", slog.String("pre", pre),
				slog.String("ip", k), slog.Any("error", err))
			continue
		}

		if telemetry.PublicIP == ip_ {
			continue
		}

		tasks = append(tasks, model.ProbeTask{
			TargetType: Node,
			Provider:   telemetry.Provider,
			IP:         telemetry.PublicIP,
			Port:       8081,
			Region:     telemetry.Continent,
		})
	}

	for _, v := range CloudStorageMap {
		tasks = append(tasks, model.ProbeTask{
			TargetType: CloudStorage,
			Provider:   v.Provider,
			IP:         v.IP,
			Port:       v.Port,
			Region:     v.Region,
			ID:         v.ID,
		})
	}

	resp.Code = 200
	resp.Msg = "成功获取节点探测任务"
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
