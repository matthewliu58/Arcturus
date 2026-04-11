package api

import (
	agg "control-plane/info-agg"
	model "control-plane/receive-info"
	routing1 "control-plane/routing"
	"control-plane/routing/graph"
	routing2 "control-plane/routing/routing"
	"control-plane/util"
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http"
	"strconv"
)

// UserRoutingAPIHandler 提供获取用户传输文件的路由信息
type UserRoutingAPIHandler struct {
	GraphManager *graph.GraphManager
	GlobalStats  *agg.GlobalStats
	Logger       *slog.Logger
}

// NewUserRoutingAPIHandler 初始化
func NewUserRoutingAPIHandler(gm *graph.GraphManager, gs *agg.GlobalStats, logger *slog.Logger) *UserRoutingAPIHandler {
	return &UserRoutingAPIHandler{
		GraphManager: gm,
		GlobalStats:  gs,
		Logger:       logger,
	}
}

// GetUserRoute 处理 POST /api/v1/routing
func (h *UserRoutingAPIHandler) GetMiddleRoute(c *gin.Context) {

	pre := c.GetHeader("X-Pre")
	if len(pre) <= 0 {
		pre = util.GenerateRandomLetters(5)
	}
	h.Logger.Info("GetMiddleRoute", slog.String("pre", pre))

	resp := model.ApiResponse{
		Code: 500,
		Msg:  "服务端内部错误",
		Data: nil,
	}

	// 解析 body JSON
	var req routing2.EndPoints
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Code = 400
		resp.Msg = "请求体解析失败：" + err.Error()
		c.JSON(http.StatusOK, resp)
		h.Logger.Warn("GetMiddleRoute parse body failed", slog.String("pre", pre), slog.Any("error", err))
		return
	}
	ip, ok := CloudStorageMap[req.Dest.Port]
	if !ok {
		resp.Code = 400
		resp.Msg = "请求体解析失败：未找到对应端口"
		c.JSON(http.StatusOK, resp)
		h.Logger.Warn("GetMiddleRoute failed", slog.String("pre", pre))
		return
	}
	req.Dest.IP = ip.IP + ":" + strconv.Itoa(ip.Port)
	h.Logger.Info("GetMiddleRoute request", slog.String("pre", pre), slog.Any("endPoints", req))

	paths := routing1.MiddleRouting(h.GraphManager, req, routing1.Shortest, pre, h.Logger)
	h.Logger.Info("GetMiddleRoute response", slog.String("pre", pre), slog.Any("routing", paths))

	resp.Code = 200
	resp.Msg = "成功获取路径"
	resp.Data = paths
	c.JSON(http.StatusOK, resp)
}

func (h *UserRoutingAPIHandler) GetLastRoute(c *gin.Context) {

	pre := c.GetHeader("X-Pre")
	if len(pre) <= 0 {
		pre = util.GenerateRandomLetters(5)
	}
	h.Logger.Info("GetLastRoute", slog.String("pre", pre))

	resp := model.ApiResponse{
		Code: 500,
		Msg:  "服务端内部错误",
		Data: nil,
	}

	// 解析 body JSON
	var req routing2.EndPoints
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Code = 400
		resp.Msg = "请求体解析失败：" + err.Error()
		c.JSON(http.StatusOK, resp)
		h.Logger.Warn("GetLastRoute failed", slog.String("pre", pre), slog.Any("error", err))
		return
	}
	h.Logger.Info("GetLastRoute request", slog.String("pre", pre), slog.Any("endPoints", req))

	paths := routing1.LastRouting(h.GraphManager, h.GlobalStats, req, routing1.Lyapunov, pre, h.Logger)
	h.Logger.Info("GetLastRoute response", slog.String("pre", pre), slog.Any("routing", paths))

	resp.Code = 200
	resp.Msg = "成功获取路径"
	resp.Data = paths
	c.JSON(http.StatusOK, resp)
}

// InitUserRoutingRouter 初始化用户路由 API 路由
func InitUserRoutingRouter(router *gin.Engine, gm *graph.GraphManager, gs *agg.GlobalStats, logger *slog.Logger) *gin.Engine {
	apiV1 := router.Group("/api/v1")
	{
		routingGroup := apiV1.Group("/routing")
		{
			handler := NewUserRoutingAPIHandler(gm, gs, logger)
			routingGroup.POST("/middle", handler.GetMiddleRoute) // POST /api/v1/routing
			routingGroup.POST("/last", handler.GetLastRoute)
		}
	}
	return router
}
