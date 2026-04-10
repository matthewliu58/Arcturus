package api

import (
	"control-plane/info-agg"
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
	CloudStorageMap map[string]CloudStorageTarget
)

// NodeProbeAPIHandler 提供获取节点探测任务的接口
type NodeProbeAPIHandler struct {
	etcdClient *clientv3.Client
	logger     *slog.Logger
}

type CloudStorageTarget struct {
	Provider string `json:"provider"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Region   string `json:"region"`
	ID       string `json:"id"`
}

func LoadCloudStorageTargetsFromExeDir() (map[string]CloudStorageTarget, error) {
	// 1. 获取可执行文件路径
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("get executable path failed: %w", err)
	}

	// 2. 解析可执行文件所在目录
	exeDir := filepath.Dir(exePath)

	// 3. 拼出配置文件完整路径
	targetFile := filepath.Join(exeDir, probeTargets)

	// 4. 读取文件
	data, err := os.ReadFile(targetFile)
	if err != nil {
		return nil, fmt.Errorf("read cloud storage targets file failed (%s): %w", targetFile, err)
	}

	// 5. 反序列化 JSON
	var targets []CloudStorageTarget
	if err := json.Unmarshal(data, &targets); err != nil {
		return nil, fmt.Errorf("unmarshal cloud storage targets failed: %w", err)
	}

	cloudStorageMap := make(map[string]CloudStorageTarget)
	for _, v := range targets {
		cloudStorageMap[v.IP] = v
	}

	return cloudStorageMap, nil
}

// NewNodeProbeAPIHandler 初始化
func NewNodeProbeAPIHandler(cli *clientv3.Client, logger *slog.Logger) *NodeProbeAPIHandler {
	return &NodeProbeAPIHandler{
		etcdClient: cli,
		logger:     logger,
	}
}

// GetProbeTasks 处理 GET /api/v1/probe/tasks
// 返回当前所有节点的探测任务信息
func (h *NodeProbeAPIHandler) GetProbeTasks(c *gin.Context) {
	resp := model.ApiResponse{
		Code: 500,
		Msg:  "服务端内部错误",
		Data: nil,
	}

	pre := util.GenerateRandomLetters(5)

	// 1. 从Etcd获取所有节点
	nodeMap, err := etcd_client.GetPrefixAll(h.etcdClient, "/routing/", pre, h.logger)
	if err != nil {
		resp.Code = 500
		resp.Msg = "获取节点信息失败：" + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	// 2. 解析每个节点JSON，生成Targets列表
	var tasks []model.ProbeTask
	//ip_, _ := util.GetPublicIP()
	ip_ := util.Config_.Node.IP.Public
	for k, nodeJson := range nodeMap {
		var telemetry info_agg.NetworkTelemetry
		if err := json.Unmarshal([]byte(nodeJson), &telemetry); err != nil {
			h.logger.Warn("解析节点JSON失败，跳过", slog.String("pre", pre),
				slog.String("ip", k), slog.Any("error", err))
			continue
		}

		if telemetry.PublicIP == ip_ {
			continue
		}

		//选取controller作为 node代理节点进行探测
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

	// 3. 返回JSON
	resp.Code = 200
	resp.Msg = "成功获取节点探测任务"
	resp.Data = tasks
	c.JSON(http.StatusOK, resp)
}

// InitNodeProbeRouter 初始化路由
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
