package api

import (
	agg "control-plane/aggregator"
	rece "control-plane/receive-info"
	routing1 "control-plane/routing"
	"control-plane/routing/graph"
	routing2 "control-plane/routing/routing"
	"control-plane/util"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type UserRoutingAPIHandler struct {
	GraphManager *graph.GraphManager
	GlobalStats  *agg.GlobalStats
	Logger       *slog.Logger
}

func NewUserRoutingAPIHandler(gm *graph.GraphManager, gs *agg.GlobalStats, logger *slog.Logger) *UserRoutingAPIHandler {
	return &UserRoutingAPIHandler{
		GraphManager: gm,
		GlobalStats:  gs,
		Logger:       logger,
	}
}

func (h *UserRoutingAPIHandler) GetMiddleRoute(c *gin.Context) {

	pre := c.Query("ip")
	pre += util.GenerateRandomLetters(5)

	h.Logger.Info("GetMiddleRoute", slog.String("pre", pre))

	resp := rece.ApiResponse{
		Code: 500,
		Msg:  "Internal server error",
		Data: nil,
	}

	var req routing2.EndPoints
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Code = 400
		resp.Msg = "Request body parsing failed: " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.Logger.Warn("GetMiddleRoute parse body failed", slog.String("pre", pre), slog.Any("error", err))
		return
	}
	source, ok := SourceTargetMap[req.Dest.Port]
	if !ok {
		resp.Code = 400
		resp.Msg = "Request body parsing failed: No corresponding port found"
		c.JSON(http.StatusOK, resp)
		h.Logger.Warn("GetMiddleRoute failed", slog.String("pre", pre))
		return
	}
	req.Dest.IP = source.IP + ":" + strconv.Itoa(source.Port)
	h.Logger.Info("GetMiddleRoute request", slog.String("pre", pre), slog.Any("endPoints", req))

	algorithm := c.Query("algorithm")
	if len(algorithm) <= 0 {
		algorithm = routing1.Shortest
	}
	paths := routing1.MiddleRouting(h.GraphManager, req, algorithm, pre, h.Logger)
	h.Logger.Info("GetMiddleRoute response", slog.String("pre", pre), slog.Any("routing", paths))

	resp.Code = 200
	resp.Msg = "Successfully obtained path"
	resp.Data = paths
	c.JSON(http.StatusOK, resp)
}

func (h *UserRoutingAPIHandler) GetLastRoute(c *gin.Context) {

	pre := c.Query("ip")
	pre += util.GenerateRandomLetters(5)

	h.Logger.Info("GetLastRoute", slog.String("pre", pre))

	resp := rece.ApiResponse{
		Code: 500,
		Msg:  "Internal server error",
		Data: nil,
	}

	var req routing2.EndPoints
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Code = 400
		resp.Msg = "Request body parsing failed: " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.Logger.Warn("GetLastRoute failed", slog.String("pre", pre), slog.Any("error", err))
		return
	}
	h.Logger.Info("GetLastRoute request", slog.String("pre", pre), slog.Any("endPoints", req))

	paths := routing1.LastRouting(h.GraphManager, h.GlobalStats, req, routing1.Lyapunov, pre, h.Logger)
	h.Logger.Info("GetLastRoute response", slog.String("pre", pre), slog.Any("routing", paths))

	resp.Code = 200
	resp.Msg = "Successfully obtained path"
	resp.Data = paths
	c.JSON(http.StatusOK, resp)
}

func InitUserRoutingRouter(router *gin.Engine, gm *graph.GraphManager,
	gs *agg.GlobalStats, logger *slog.Logger) *gin.Engine {
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
