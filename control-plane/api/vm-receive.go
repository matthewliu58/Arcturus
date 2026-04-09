package api

import (
	model "control-plane/receive-info"
	"control-plane/storage"
	"control-plane/util"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ReportHandler 接收VM上报数据的Handler
// 职责：解析上报请求、校验参数、调用存储层、返回统一响应
type VmReceiveAPIHandler struct {
	storage    storage.Storage // 注入文件存储实现（依赖倒置，解耦具体存储）
	logger     *slog.Logger
	activityVM *util.SafeMap
}

// NewReportHandler 初始化ReportHandler
// 参数：存储层实现实例
// 返回：初始化后的ReportHandler指针
func NewVmReceiveAPIHandler(s storage.Storage, l *slog.Logger) *VmReceiveAPIHandler {
	return &VmReceiveAPIHandler{storage: s, logger: l, activityVM: util.NewSafeMap()}
}

// PostVMReport 处理POST /api/v1/vm/report请求
// 核心：请求体是ApiResponse（Data=VMReport），响应体也是ApiResponse（Data=填充后的VMReport）
func (h *VmReceiveAPIHandler) PostVMReceive(c *gin.Context) {

	pre := util.GenerateRandomLetters(5)

	// 1. 初始化响应体（默认失败，后续覆盖）
	resp := model.ApiResponse{
		Code: 500,
		Msg:  "服务端内部错误",
		Data: nil,
	}

	// 2.先绑定外层ApiResponse
	var req model.ApiResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Code = 400
		resp.Msg = "请求格式错误：不是合法的ApiResponse结构 - " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	// 3. 解析内层Data为VMReport
	// 3.1 先将Data转为JSON字节（兼容各种数据类型）
	reqDataBytes, err := json.Marshal(req.Data)
	if err != nil {
		resp.Code = 400
		resp.Msg = "请求Data字段格式错误：无法序列化为JSON - " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}
	// 3.2 反序列化为VMReport
	var reportData model.VMReport
	if err := json.Unmarshal(reqDataBytes, &reportData); err != nil {
		resp.Code = 400
		resp.Msg = "Data字段解析失败：不是合法的VMReport结构 - " + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	// 4. 校验VMReport核心字段
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

	// 校验失败返回
	if len(validateErrors) > 0 {
		resp.Code = 400
		resp.Msg = "VMReport参数校验失败：" + strings.Join(validateErrors, "；")
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	// 5. 字段兜底填充（补全缺失的必填项）
	if reportData.CollectTime.IsZero() {
		reportData.CollectTime = time.Now().UTC()
	}
	if reportData.ReportID == "" {
		reportData.ReportID = uuid.NewString()
	}
	if reportData.Network.PortCount < 0 {
		reportData.Network.PortCount = 0
	}

	// 6. 保存数据到存储层
	if _, err := h.storage.Save(&reportData, pre); err != nil {
		resp.Code = 500
		resp.Msg = "数据保存失败：" + err.Error()
		c.JSON(http.StatusOK, resp)
		h.logger.Error(resp.Msg)
		return
	}

	//保存最近更新的vm列表 主要关注CollectTime 更新最近10s上报的vm
	//h.activityVM.Set(reportData.VMID, reportData)

	// 7. 构造成功响应（外层ApiResponse + 内层Data=填充后的VMReport）
	resp.Code = 200
	resp.Msg = "VM信息上报成功"
	resp.Data = reportData // 响应的Data放回完整的VMReport

	b, _ := json.Marshal(reportData)
	h.logger.Info(string(b))

	// 8. 返回最终响应
	c.JSON(http.StatusOK, resp)
}

// NewRouter 初始化路由（无修改）
func InitVmReceiveAPIRouter(router *gin.Engine, s *storage.FileStorage, logger *slog.Logger) *gin.Engine {

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
