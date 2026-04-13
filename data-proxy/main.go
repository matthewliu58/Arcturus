package main

import (
	"context"
	"data-proxy/aggregator"
	"data-proxy/backsourcer"
	"data-proxy/config"
	"data-proxy/disaggregator"
	"data-proxy/server"
	manager "data-proxy/tunnel-manager"
	"github.com/gin-gonic/gin"
	"gopkg.in/natefinch/lumberjack.v2"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// QUIC 退出信号
var quicExit = make(chan error, 1)

// 自定义Handler：带文件/行号
type SourceHandler struct {
	handler slog.Handler
}

func (h *SourceHandler) Handle(ctx context.Context, r slog.Record) error {
	fs := runtime.CallersFrames([]uintptr{r.PC})
	frame, _ := fs.Next()
	fileName := filepath.Base(frame.File)

	r.AddAttrs(
		slog.String("file", fileName),
		slog.Int("line", frame.Line),
		slog.String("func", frame.Func.Name()),
	)
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
	os.MkdirAll(logDir, 0755)

	appLog := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "app.log"),
		MaxSize:    128,   // 单个文件最大 128MB
		MaxBackups: 10,    // 最多保留 10 个文件
		MaxAge:     30,    // 最多保留 30 天
		Compress:   false, // 不需要压缩
	}

	accessLog := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "access.log"),
		MaxSize:    128,
		MaxBackups: 20,
		MaxAge:     30,
		Compress:   false,
	}

	// 包装 slog
	logHandler := slog.NewTextHandler(appLog, &slog.HandlerOptions{Level: slog.LevelInfo})
	accessHandler := slog.NewTextHandler(accessLog, &slog.HandlerOptions{Level: slog.LevelInfo})

	logger := slog.New(&SourceHandler{handler: logHandler})
	accessLogger := slog.New(&SourceHandler{handler: accessHandler})

	// 全局
	slog.SetDefault(logger)

	pre := "init"
	var err error
	config.Config_, err = config.ReadYamlConfig(logger)
	if err != nil {
		logger.Error("read config failed", slog.String("pre", pre), "err", err)
		return
	}

	// 启动正向聚合器
	aggregator.GlobalAggRequest = aggregator.NewAggregator(pre, logger)
	aggregator.GlobalAggRequest.Start()

	// 启动反向聚合器
	aggregator.GlobalAggResponse = aggregator.NewAggregator(pre, logger)
	aggregator.GlobalAggResponse.Start()

	// 启动反聚合器
	disaggregator.GlobalDisagg = disaggregator.NewDisaggregator(pre, logger)

	// 启动 backsourcer
	backsourcer.GlobalBackSourcer = backsourcer.NewBackSourcer()

	// 启动限流器
	server.InitRateLimiter(config.Config_.RateLimit)

	// 启动 tunnel manager
	manager.TunnelMgr = manager.NewTunnelManager(pre, logger)

	// 启动 quic listener（goroutine 运行，崩溃时通过 channel 通知 main）
	go func() {
		quicExit <- manager.ListenAndServeQUIC(HandleQUICPacket, pre, logger)
	}()

	for _, port := range config.Config_.ListenPorts {
		// 启动 TCP server（先创建 listener，再启动 goroutine）
		if err = server.StartServerWithMgr(port, pre, accessLogger, logger); err != nil {
			logger.Error("TCP server start failed", slog.String("pre", pre), "err", err)
			return
		}
		go server.StartServerRun(port, pre, accessLogger, logger)
	}

	// Gin
	router := gin.Default()
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, "success")
	})

	// TCP Server 管理接口
	server.ServerManager(router)

	// Gin 用 goroutine 运行
	go func() {
		port := "7095"
		logger.Info("Listening", slog.String("pre", pre), "port", port)
		if err = router.Run(":" + port); err != nil {
			logger.Error("Gin Run failed", slog.String("pre", pre), "err", err)
		}
	}()

	// 等待退出信号：QUIC 崩溃会导致程序退出
	select {
	case err = <-quicExit:
		if err != nil {
			logger.Error("QUIC server crashed, exiting", slog.String("pre", pre), "err", err)
		}
	}
}
