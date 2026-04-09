package reporter

import (
	"bytes"
	model "data-plane/report-info"
	"data-plane/report-info/middle-collector"
	"data-plane/util"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const (
	ReportURL      = "/api/v1/vm/receive" // 控制平面地址
	ReportInterval = 10 * time.Second     // 上报周期
)

// HTTPReporter HTTP上报器
type HTTPReporter struct {
	client *http.Client
}

// NewHTTPReporter 初始化上报器
func NewHTTPReporter() *HTTPReporter {
	return &HTTPReporter{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Report 上报VM信息（按ApiResponse格式封装）
func (r *HTTPReporter) Report(controlHost, pre string, vmReport *model.VMReport) error {
	// 1. 填充ReportID（若为空）
	if vmReport.ReportID == "" {
		vmReport.ReportID = uuid.NewString()
	}

	// 2. 构造外层ApiResponse请求体
	reqBody := model.ApiResponse{
		Code: 200, // 客户端默认填200
		Msg:  "VM信息上报请求",
		Data: vmReport,
	}

	// 3. 序列化为JSON
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	// 4. 发送POST请求
	resp, err := r.client.Post(controlHost+ReportURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 5. 解析响应（可选，验证上报结果）
	var respBody model.ApiResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return err
	}

	if respBody.Code != 200 {
		return fmt.Errorf("上报失败：%s", respBody.Msg)
	}

	return nil
}

func ReportCycle(controlHost, pre string, logger *slog.Logger) {
	// 1. 初始化采集器和上报器
	vmCollector := middle_collector.NewVMCollector()
	httpReporter := NewHTTPReporter()

	// 2. 启动定时上报任务
	ticker := time.NewTicker(ReportInterval)
	defer ticker.Stop()

	logger.Info(
		"数据平面启动，开始定时上报", slog.String("pre", pre),
		slog.Duration("report_interval", ReportInterval),
		slog.String("report_url", controlHost+ReportURL),
	)

	// 3. 立即执行一次上报，然后按周期执行
	//reportOnce(vmCollector, httpReporter, logger)

	for range ticker.C {
		reportOnce(controlHost, vmCollector, httpReporter, logger)
	}
}

// reportOnce 单次上报逻辑
func reportOnce(controlHost string, collector *middle_collector.VMCollector, reporter *HTTPReporter, logger *slog.Logger) {

	pre := util.GenerateRandomLetters(5)

	// 1. 采集信息
	logger.Info("开始采集VM信息...", slog.String("pre", pre))
	vmReport, err := collector.Collect(pre, logger)
	if err != nil {
		logger.Error("采集失败", slog.String("pre", pre), slog.Any("err", err))
		return
	}

	// 2. 上报信息
	b, _ := json.Marshal(vmReport)
	logger.Info("开始上报VM信息", slog.String("pre", pre), slog.String("data", string(b)))

	err = reporter.Report(controlHost, pre, vmReport)
	if err != nil {
		logger.Error("上报失败", slog.String("pre", pre), slog.Any("err", err))
		return
	}

	logger.Info("上报成功", slog.String("pre", pre), slog.String("ReportID", vmReport.ReportID))
}
