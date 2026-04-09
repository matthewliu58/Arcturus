package main

import (
	"context"
	"data-plane/probing"
	"data-plane/report-info/reporter"
	"data-plane/util"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type SourceHandler struct {
	handler slog.Handler
}

func (h *SourceHandler) Handle(ctx context.Context, r slog.Record) error {
	// 采集调用日志的位置（跳过当前Handler的栈帧，取真实业务代码的位置）
	fs := runtime.CallersFrames([]uintptr{r.PC})
	frame, _ := fs.Next()

	// 只保留文件名（去掉全路径）
	fileName := filepath.Base(frame.File)

	// 向日志记录中添加源位置字段
	r.AddAttrs(
		slog.String("file", fileName),          // 文件名
		slog.Int("line", frame.Line),           // 行号
		slog.String("func", frame.Func.Name()), // 函数名（可选）
	)

	// 交给底层TextHandler输出
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

	logDir := filepath.Join(".", "log")
	if err := os.MkdirAll(logDir, os.ModePerm); err != nil {
		panic("无法创建日志目录: " + err.Error())
	}
	logFilePath := filepath.Join(logDir, "app.log")
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic("无法打开日志文件: " + err.Error())
	}

	baseHandler := slog.NewTextHandler(logFile, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})
	logger := slog.New(&SourceHandler{handler: baseHandler})
	slog.SetDefault(logger)

	logPre := "init"
	util.Config_, err = util.ReadYamlConfig(logger)
	if err != nil {
		logger.Error("read config failed", slog.String("pre", logPre), slog.Any("err", err))
		return
	} else {
		b, _ := json.Marshal(util.Config_)
		logger.Info("读取配置文件成功", slog.String("pre", logPre),
			slog.String("config", string(b)))
	}

	go reporter.ReportCycle(util.Config_.ControlHost, logPre, logger)

	//启动探测逻辑
	cfg := probing.Config{
		Concurrency: 4,
		Timeout:     2 * time.Second,
		Interval:    5 * time.Second,
		Attempts:    5, // 每轮尝试次数
	}
	ctx := context.Background()
	probing.StartProbePeriodically(ctx, util.Config_.ControlHost, cfg, logPre, logger)

	router := gin.Default()
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, "success")
	})

	logger.Info("API端口启动", slog.String("pre", logPre), slog.String("port", ":7082"))
	if err := router.Run(":7082"); err != nil {
		logger.Error("API服务启动失败", slog.String("pre", logPre), slog.Any("err", err))
		return
	}
}
