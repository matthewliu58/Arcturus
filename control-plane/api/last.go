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
	lastExpireTime = 1
)

type LastReceiveAPIHandler struct {
	etcdCli *clientv3.Client
	logger  *slog.Logger
}

func NewLastReceiveAPIHandler(cli *clientv3.Client, l *slog.Logger) *LastReceiveAPIHandler {
	return &LastReceiveAPIHandler{etcdCli: cli, logger: l}
}

func (h *LastReceiveAPIHandler) PostLastReceive(c *gin.Context) {

	pre := c.Query("ip")
	pre += util.GenerateRandomLetters(5)

	resp := receive_info.ApiResponse{
		Code: 500,
		Msg:  "Internal server error",
		Data: nil,
	}

	var req receive_info.ApiResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Code = 400
		resp.Msg = "Request format error: " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg, slog.String("pre", pre))
		return
	}

	reqDataBytes, err := json.Marshal(req.Data)
	if err != nil {
		resp.Code = 400
		resp.Msg = "Request Data field format error: " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg, slog.String("pre", pre))
		return
	}

	var lastStats receive_info.LastTelemetry
	if err = json.Unmarshal(reqDataBytes, &lastStats); err != nil {
		resp.Code = 400
		resp.Msg = "Data field parsing failed: " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg, slog.String("pre", pre))
		return
	}

	ip := util.Config_.Node.IP.Public
	key := fmt.Sprintf("/routing-last/%s", ip)
	if err = etcd_client.PutKeyWithLease(h.etcdCli, key, string(reqDataBytes), int64(60*lastExpireTime), pre, h.logger); err != nil {
		h.logger.Error("Failed to store LastStats to etcd", slog.String("pre", pre), slog.Any("error", err))
	}

	h.logger.Info("LastStats received", slog.String("pre", pre), slog.String("ip", ip), slog.String("key", key))

	resp.Code = 200
	resp.Msg = "LastStats reported successfully"
	resp.Data = lastStats

	c.JSON(http.StatusOK, resp)
}

func InitLastReceiveAPIRouter(router *gin.Engine, cli *clientv3.Client, logger *slog.Logger) *gin.Engine {
	r := router
	apiV1 := r.Group("/api/v1")
	{
		handler := NewLastReceiveAPIHandler(cli, logger)
		apiV1.POST("/last/receive", handler.PostLastReceive)
	}
	return r
}
