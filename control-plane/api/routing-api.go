package api

import (
	model "control-plane/receive-info"
	"control-plane/routing"
	"control-plane/util"
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http"
)

// UserRoutingAPIHandler 提供获取用户传输文件的路由信息
type UserRoutingAPIHandler struct {
	GM     *routing.GraphManager
	Logger *slog.Logger
}

// NewUserRoutingAPIHandler 初始化
func NewUserRoutingAPIHandler(gm *routing.GraphManager, logger *slog.Logger) *UserRoutingAPIHandler {
	return &UserRoutingAPIHandler{
		GM:     gm,
		Logger: logger,
	}
}

// GetUserRoute 处理 POST /api/v1/routing
func (h *UserRoutingAPIHandler) GetUserRoute(c *gin.Context) {

	pre := c.GetHeader("X-Pre")
	if len(pre) <= 0 {
		pre = util.GenerateRandomLetters(5)
	}

	h.Logger.Info("GetUserRoute", slog.String("pre", pre))

	resp := model.ApiResponse{
		Code: 500,
		Msg:  "服务端内部错误",
		Data: nil,
	}

	// 解析 header 信息（如果有）
	filename := c.GetHeader("X-File-Name")

	// 解析 body JSON
	var req routing.EndPoints
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Code = 400
		resp.Msg = "请求体解析失败：" + err.Error()
		c.JSON(http.StatusOK, resp)
		h.Logger.Warn("PostUserRoute parse body failed",
			slog.String("pre", pre), slog.Any("error", err))
		return
	}

	h.Logger.Info("UserRoute POST request", slog.String("pre", pre),
		slog.String("fileName", filename), slog.Any("endPoints", req))

	//调用 GraphManager 获取最优路径 可以把 GetBestPath 改成支持从客户端到服务器
	paths := h.GM.Routing(req, pre, h.Logger)

	h.Logger.Info("UserRoute POST response", slog.String("pre", pre),
		slog.Any("routing", paths))

	resp.Code = 200
	resp.Msg = "成功获取路径"
	resp.Data = paths
	c.JSON(http.StatusOK, resp)
}

// InitUserRoutingRouter 初始化用户路由 API 路由
func InitUserRoutingRouter(router *gin.Engine, gm *routing.GraphManager, logger *slog.Logger) *gin.Engine {
	apiV1 := router.Group("/api/v1")
	{
		routingGroup := apiV1.Group("/routing")
		{
			handler := NewUserRoutingAPIHandler(gm, logger)
			routingGroup.POST("", handler.GetUserRoute) // POST /api/v1/routing
		}
	}
	return router
}
