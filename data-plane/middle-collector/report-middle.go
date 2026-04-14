package middle_collector

import (
	"bytes"
	model "data-plane/report-info"
	"data-plane/util"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const (
	ReportURL      = "/api/v1/vm/receive"
	ReportInterval = 10 * time.Second
)

type HTTPReporter struct {
	client *http.Client
}

func NewHTTPReporter() *HTTPReporter {
	return &HTTPReporter{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (r *HTTPReporter) Report(controlHost, pre string, vmReport *model.VMReport) error {

	if vmReport.ReportID == "" {
		vmReport.ReportID = uuid.NewString()
	}

	reqBody := model.ApiResponse{
		Code: 200,
		Msg:  "VM reporting msg",
		Data: vmReport,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	resp, err := r.client.Post(controlHost+ReportURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var respBody model.ApiResponse
	if err = json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return err
	}

	if respBody.Code != 200 {
		return fmt.Errorf("report failed, %s", respBody.Msg)
	}

	return nil
}

func ReportCycle(controlHost, pre string, logger *slog.Logger) {

	vmCollector := NewVMCollector()
	httpReporter := NewHTTPReporter()

	ticker := time.NewTicker(ReportInterval)
	defer ticker.Stop()

	logger.Info(
		"数据平面启动，开始定时上报", slog.String("pre", pre),
		slog.Duration("report_interval", ReportInterval),
		slog.String("report_url", controlHost+ReportURL),
	)

	//reportOnce(vmCollector, httpReporter, logger)

	for range ticker.C {
		reportOnce(controlHost, vmCollector, httpReporter, logger)
	}
}

func reportOnce(controlHost string, collector *VMCollector, reporter *HTTPReporter, logger *slog.Logger) {

	pre := util.GenerateRandomLetters(5)

	logger.Info("开始采集VM信息...", slog.String("pre", pre))
	vmReport, err := collector.Collect(pre, logger)
	if err != nil {
		logger.Error("采集失败", slog.String("pre", pre), slog.Any("err", err))
		return
	}
	
	b, _ := json.Marshal(vmReport)
	logger.Info("开始上报VM信息", slog.String("pre", pre), slog.String("data", string(b)))

	err = reporter.Report(controlHost, pre, vmReport)
	if err != nil {
		logger.Error("上报失败", slog.String("pre", pre), slog.Any("err", err))
		return
	}

	logger.Info("上报成功", slog.String("pre", pre), slog.String("ReportID", vmReport.ReportID))
}
