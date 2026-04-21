package api

import (
	model "control-plane/receive-info"
	"control-plane/util"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type VmReceiveAPIHandler struct {
	storage    util.Storage
	logger     *slog.Logger
	activityVM *util.SafeMap
}

func NewVmReceiveAPIHandler(s util.Storage, l *slog.Logger) *VmReceiveAPIHandler {
	return &VmReceiveAPIHandler{storage: s, logger: l, activityVM: util.NewSafeMap()}
}

func (h *VmReceiveAPIHandler) PostVMReceive(c *gin.Context) {

	pre := c.Query("ip")
	pre += util.GenerateRandomLetters(5)

	resp := model.ApiResponse{
		Code: 500,
		Msg:  "Internal server error",
		Data: nil,
	}

	var req model.ApiResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Code = 400
		resp.Msg = "Request format error: Not a valid ApiResponse structure - " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	reqDataBytes, err := json.Marshal(req.Data)
	if err != nil {
		resp.Code = 400
		resp.Msg = "Request Data field format error: Cannot serialize to JSON - " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	var reportData model.VMReport
	if err = json.Unmarshal(reqDataBytes, &reportData); err != nil {
		resp.Code = 400
		resp.Msg = "Data field parsing failed: Not a valid VMReport structure - " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	var validateErrors []string
	if reportData.VMID == "" {
		validateErrors = append(validateErrors, "VMID cannot be empty")
	}
	if reportData.CPU.PhysicalCore < 0 {
		validateErrors = append(validateErrors, "CPU physical core count cannot be negative")
	}
	if reportData.CPU.LogicalCore < 0 {
		validateErrors = append(validateErrors, "CPU logical core count cannot be negative")
	}
	if reportData.Network.PublicIP == "" {
		validateErrors = append(validateErrors, "Public IP (public_ip) cannot be empty, use \"no-public-ip\" if none")
	}
	if reportData.Memory.Total == 0 {
		validateErrors = append(validateErrors, "Total memory (total) cannot be 0")
	}

	if len(validateErrors) > 0 {
		resp.Code = 400
		resp.Msg = "VMReport parameter validation failed: " + strings.Join(validateErrors, "; ")
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	if reportData.CollectTime.IsZero() {
		reportData.CollectTime = time.Now().UTC()
	}
	if reportData.ReportID == "" {
		reportData.ReportID = uuid.NewString()
	}
	if reportData.Network.PortCount < 0 {
		reportData.Network.PortCount = 0
	}

	if _, err := h.storage.Save(&reportData, pre); err != nil {
		resp.Code = 500
		resp.Msg = "Data save failed: " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	resp.Code = 200
	resp.Msg = "VM information reported successfully"
	resp.Data = reportData

	b, _ := json.Marshal(reportData)
	h.logger.Info(string(b))

	c.JSON(http.StatusOK, resp)
}

func InitVmReceiveAPIRouter(router *gin.Engine, s *util.FileStorage, logger *slog.Logger) *gin.Engine {

	r := router
	apiV1 := r.Group("/api/v1")
	{
		vmGroup := apiV1.Group("/vm")
		{
			handler := NewVmReceiveAPIHandler(s, logger)
			vmGroup.POST("/receive", handler.PostVMReceive)
		}
	}
	return r
}
