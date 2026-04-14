package api

import (
	model "control-plane/receive-info"
	"control-plane/util"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
	"strings"
	"time"
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

	pre := util.GenerateRandomLetters(5)

	resp := model.ApiResponse{
		Code: 500,
		Msg:  "服务端内部错误",
		Data: nil,
	}

	var req model.ApiResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Code = 400
		resp.Msg = "请求格式错误：不是合法的ApiResponse结构 - " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	reqDataBytes, err := json.Marshal(req.Data)
	if err != nil {
		resp.Code = 400
		resp.Msg = "请求Data字段格式错误：无法序列化为JSON - " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	var reportData model.VMReport
	if err = json.Unmarshal(reqDataBytes, &reportData); err != nil {
		resp.Code = 400
		resp.Msg = "Data字段解析失败：不是合法的VMReport结构 - " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	var validateErrors []string
	if reportData.VMID == "" {
		validateErrors = append(validateErrors, "VMID不能为空")
	}
	if reportData.CPU.PhysicalCore < 0 {
		validateErrors = append(validateErrors, "CPU物理核数不能为负数")
	}
	if reportData.CPU.LogicalCore < 0 {
		validateErrors = append(validateErrors, "CPU逻辑核数不能为负数")
	}
	if reportData.Network.PublicIP == "" {
		validateErrors = append(validateErrors, "外网IP（public_ip）不能为空，无则填\"no-public-ip\"")
	}
	if reportData.Memory.Total == 0 {
		validateErrors = append(validateErrors, "总内存（total）不能为0")
	}

	if len(validateErrors) > 0 {
		resp.Code = 400
		resp.Msg = "VMReport参数校验失败：" + strings.Join(validateErrors, "；")
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
		resp.Msg = "数据保存失败：" + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	//h.activityVM.Set(reportData.VMID, reportData)

	resp.Code = 200
	resp.Msg = "VM信息上报成功"
	resp.Data = reportData

	b, _ := json.Marshal(reportData)
	h.logger.Info(string(b))

	c.JSON(http.StatusOK, resp)
}

func InitVmReceiveAPIRouter(router *gin.Engine, s *util.FileStorage, logger *slog.Logger) *gin.Engine {

	r := router
	// 上报接口
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
