package main

import (
	"context"
	middle "data-plane/collector"
	last "data-plane/edge-domain"
	"data-plane/probing"
	"data-plane/util"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

type SourceHandler struct {
	handler slog.Handler
}

func (h *SourceHandler) Handle(ctx context.Context, r slog.Record) error {
	fs := runtime.CallersFrames([]uintptr{r.PC})
	frame, _ := fs.Next()
	fileName := filepath.Base(frame.File)
	r.AddAttrs(slog.String("file", fileName),
		slog.Int("line", frame.Line), slog.String("func", frame.Func.Name()))
	return h.handler.Handle(ctx, r)
}

func (h *SourceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *SourceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SourceHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h *SourceHandler) WithGroup(name string) slog.Handler {
	return &SourceHandler{handler: h.handler.WithGroup(name)}
}

func main() {
	pre := "main"

	logDir := filepath.Join(".", "log")
	_ = os.MkdirAll(logDir, os.ModePerm)
	logFilePath := filepath.Join(logDir, "app.log")
	logFile, _ := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)

	baseHandler := slog.NewTextHandler(logFile, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})
	logger := slog.New(&SourceHandler{handler: baseHandler})
	slog.SetDefault(logger)

	var err error
	util.Config_, err = util.ReadYamlConfig(logger)
	if err != nil {
		logger.Error("Read config failed", slog.String("pre", pre), slog.Any("err", err))
		return
	}
	logger.Info("Successfully read the configuration file",
		slog.String("pre", pre))

	if err = util.InitIPInfo(pre, logger); err != nil {
		logger.Warn("IP library initialization failed", slog.String("pre", pre), slog.Any("err", err))
	}

	if err = util.LoadCountryContinent(pre, logger); err != nil {
		logger.Warn("Failed to read country information file", slog.String("pre", pre), slog.Any("err", err))
	}

	go last.LastTelemetryReporter(pre, logger)

	middle.StartCPUSampler()
	go middle.VMTelemetryReporter(pre, logger)

	probing.StartProbePeriodically(context.Background(), util.Config_.ControlHost,
		probing.Config{
			Concurrency: 4,
			Timeout:     2 * time.Second,
			Interval:    5 * time.Second,
			Attempts:    5,
		}, pre, logger)

	router := gin.Default()
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, "success")
	})
	ipGroup := router.Group("/ip")
	{
		ipGroup.GET("/info", getIPInfoHandler(logger))
	}

	logger.Info("API port started", slog.String("pre", pre), slog.String("port", ":7082"))
	if err = router.Run(":7082"); err != nil {
		logger.Error("Service startup failed", slog.String("pre", pre), slog.Any("err", err))
		return
	}
}

// getIPInfoHandler GET /ip/info?ip=1.1.1.1
func getIPInfoHandler(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {

		ip := c.Query("ip")
		if ip == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "ip parameter is required",
			})
			return
		}

		ipInfo, err := util.GetIPInfo(ip, ip, logger)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, ipInfo)
	}
}
