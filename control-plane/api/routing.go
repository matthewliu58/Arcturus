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
		algorithm = routing1.KShortest
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

	algorithm := c.Query("algorithm")
	if len(algorithm) <= 0 {
		algorithm = routing1.Lyapunov
	}

	// Get weights from query parameters
	cpuWeight := 0.5
	latencyWeight := 0.5
	if algorithm == routing1.JointCpuLatency {
		cpuStr := c.Query("cpu_weight")
		latStr := c.Query("latency_weight")
		if cpuStr != "" && latStr != "" {
			cpuVal, cpuErr := strconv.ParseFloat(cpuStr, 64)
			latVal, latErr := strconv.ParseFloat(latStr, 64)
			if cpuErr == nil && latErr == nil && cpuVal >= 0 && latVal >= 0 {
				cpuWeight = cpuVal
				latencyWeight = latVal
			}
		}
	}

	paths := routing1.LastRouting(h.GraphManager, h.GlobalStats, req, algorithm, cpuWeight, latencyWeight, pre, h.Logger)
	h.Logger.Info("GetLastRoute response", slog.String("pre", pre), slog.Any("routing", paths))

	resp.Code = 200
	resp.Msg = "Successfully obtained path"
	resp.Data = paths
	c.JSON(http.StatusOK, resp)
}

// MultiDestRequest is the request body for multi-destination routing.
type MultiDestRequest struct {
	Source routing2.EndPoint   `json:"source" binding:"required"`
	Dests  []routing2.EndPoint `json:"dests" binding:"required"`
}

func (h *UserRoutingAPIHandler) GetMiddleRouteMulti(c *gin.Context) {
	pre := c.Query("ip")
	pre += util.GenerateRandomLetters(5)

	h.Logger.Info("GetMiddleRouteMulti", slog.String("pre", pre))

	resp := rece.ApiResponse{
		Code: 500,
		Msg:  "Internal server error",
		Data: nil,
	}

	var req MultiDestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Code = 400
		resp.Msg = "Request body parsing failed: " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.Logger.Warn("GetMiddleRouteMulti parse body failed", slog.String("pre", pre), slog.Any("error", err))
		return
	}

	h.Logger.Info("GetMiddleRouteMulti request", slog.String("pre", pre),
		slog.Any("source", req.Source), slog.Any("dests", req.Dests))

	algorithm := c.Query("algorithm")
	if len(algorithm) <= 0 {
		algorithm = routing1.ONEWANMulti
	}

	// Extract IP strings from EndPoint array
	var destIPs []string
	for _, dest := range req.Dests {
		destIPs = append(destIPs, dest.IP)
	}

	paths := routing1.MiddleRoutingMulti(h.GraphManager, req.Source.IP, destIPs,
		algorithm, pre, h.Logger)
	h.Logger.Info("GetMiddleRouteMulti response", slog.String("pre", pre),
		slog.Any("routing", paths))

	resp.Code = 200
	resp.Msg = "Successfully obtained paths"
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
			routingGroup.POST("/middle", handler.GetMiddleRoute)            // POST /api/v1/routing/middle
			routingGroup.POST("/middle/multi", handler.GetMiddleRouteMulti) // POST /api/v1/routing/middle/multi
			routingGroup.POST("/last", handler.GetLastRoute)
		}
	}
	return router
}
