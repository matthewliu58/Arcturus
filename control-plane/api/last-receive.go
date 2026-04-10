package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"control-plane/receive-info"
	"control-plane/sync/etcd_client"
	"control-plane/util"
	"github.com/gin-gonic/gin"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	lastExpireTime = 1 // last stats 过期时间（分钟）
)

// LastReceiveAPIHandler 接收 LastStats 上报的 Handler
type LastReceiveAPIHandler struct {
	etcdCli *clientv3.Client
	logger  *slog.Logger
}

// NewLastReceiveAPIHandler 初始化 LastReceiveAPIHandler
func NewLastReceiveAPIHandler(cli *clientv3.Client, l *slog.Logger) *LastReceiveAPIHandler {
	return &LastReceiveAPIHandler{etcdCli: cli, logger: l}
}

// PostLastReceive 处理 POST /api/v1/last/receive 请求
func (h *LastReceiveAPIHandler) PostLastReceive(c *gin.Context) {

	pre := util.GenerateRandomLetters(5)

	// 1. 初始化响应体
	resp := receive_info.ApiResponse{
		Code: 500,
		Msg:  "服务端内部错误",
		Data: nil,
	}

	// 2. 绑定外层 ApiResponse
	var req receive_info.ApiResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Code = 400
		resp.Msg = "请求格式错误：" + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg, slog.String("pre", pre))
		return
	}

	// 3. 解析内层 Data 为 LastStats
	reqDataBytes, err := json.Marshal(req.Data)
	if err != nil {
		resp.Code = 400
		resp.Msg = "请求 Data 字段格式错误：" + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg, slog.String("pre", pre))
		return
	}

	var lastStats receive_info.LastStats
	if err := json.Unmarshal(reqDataBytes, &lastStats); err != nil {
		resp.Code = 400
		resp.Msg = "Data 字段解析失败：" + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg, slog.String("pre", pre))
		return
	}

	// 4. 存储到 etcd
	ip := util.Config_.Node.IP.Public
	key := fmt.Sprintf("/routing/last/%s", ip)
	if err := etcd_client.PutKeyWithLease(h.etcdCli, key, string(reqDataBytes), int64(60*lastExpireTime), pre, h.logger); err != nil {
		h.logger.Error("存储 LastStats 到 etcd 失败", slog.String("pre", pre), slog.Any("error", err))
	}

	// 5. 打印日志
	h.logger.Info("LastStats received", slog.String("pre", pre), slog.String("ip", ip), slog.String("key", key))

	// 6. 构造成功响应
	resp.Code = 200
	resp.Msg = "LastStats 上报成功"
	resp.Data = lastStats

	c.JSON(http.StatusOK, resp)
}

// InitLastReceiveAPIRouter 初始化路由
func InitLastReceiveAPIRouter(router *gin.Engine, cli *clientv3.Client, logger *slog.Logger) *gin.Engine {
	r := router
	apiV1 := r.Group("/api/v1")
	{
		handler := NewLastReceiveAPIHandler(cli, logger)
		apiV1.POST("/last/receive", handler.PostLastReceive)
	}
	return r
}
