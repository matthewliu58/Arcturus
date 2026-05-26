package collector

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
	ReportInterval = 5 * time.Second
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

func (r *HTTPReporter) Report(pre string, vmReport *model.VMReport) error {

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

	url := fmt.Sprintf("%s?ip=%s", util.Config_.ControlHost+ReportURL, util.Config_.Node.IP.Public)
	resp, err := r.client.Post(url, "application/json", bytes.NewBuffer(jsonData))
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

func VMTelemetryReporter(pre string, logger *slog.Logger) {

	vmCollector := NewVMCollector()
	httpReporter := NewHTTPReporter()

	ticker := time.NewTicker(ReportInterval)
	defer ticker.Stop()

	logger.Info("data plane started, starting scheduled reporting", slog.String("pre", pre))

	for range ticker.C {
		reportOnce(vmCollector, httpReporter, logger)
	}
}

func reportOnce(collector *VMCollector, reporter *HTTPReporter, logger *slog.Logger) {

	pre := util.GenerateRandomLetters(5)

	logger.Info("start collecting VM information...", slog.String("pre", pre))
	vmReport, err := collector.Collect(pre, logger)
	if err != nil {
		logger.Error("collection failed", slog.String("pre", pre), slog.Any("err", err))
		return
	}

	b, _ := json.Marshal(vmReport)
	logger.Info("start reporting VM information", slog.String("pre", pre), slog.String("data", string(b)))

	err = reporter.Report(pre, vmReport)
	if err != nil {
		logger.Error("reporting failed", slog.String("pre", pre), slog.Any("err", err))
		return
	}

	logger.Info("reporting successful", slog.String("pre", pre), slog.String("ReportID", vmReport.ReportID))
}
