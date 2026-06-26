package main

import (
	"context"
	"data-proxy/aggregator"
	"data-proxy/backsourcer"
	"data-proxy/config"
	"data-proxy/disaggregator"
	"data-proxy/server"
	manager "data-proxy/tunnel-manager"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gin-gonic/gin"
	"gopkg.in/natefinch/lumberjack.v2"
)

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
	pre := "main"

	// Set GOMAXPROCS to number of CPU cores for better performance
	cpuCount := runtime.NumCPU()
	runtime.GOMAXPROCS(cpuCount)

	logDir := filepath.Join(".", "log")
	os.MkdirAll(logDir, 0755)

	appLog := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "app.log"),
		MaxSize:    128,
		MaxBackups: 10,
		MaxAge:     30,
		Compress:   false,
	}

	accessLog := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "access.log"),
		MaxSize:    128,
		MaxBackups: 20,
		MaxAge:     30,
		Compress:   false,
	}

	logHandler := slog.NewTextHandler(appLog, &slog.HandlerOptions{Level: slog.LevelDebug})
	accessHandler := slog.NewTextHandler(accessLog, &slog.HandlerOptions{Level: slog.LevelInfo})

	logger := slog.New(&SourceHandler{handler: logHandler})
	accessLogger := slog.New(&SourceHandler{handler: accessHandler})

	slog.SetDefault(logger)

	var err error
	config.Config_, err = config.ReadYamlConfig(logger)
	if err != nil {
		logger.Error("read config failed", slog.String("pre", pre), "err", err)
		return
	}

	for _, protocol := range []string{"tcp", "udp"} {
		server.ServerMap[protocol] = server.InitServerInterface(protocol, pre, logger)
		if server.ServerMap[protocol].Operate == nil {
			logger.Error("server handler init failed", slog.String("pre", pre), "err", err)
		}
		backsourcer.BackSourcerMap[protocol] = backsourcer.NewBackSourcer(protocol, pre, logger)
	}

	aggregator.GlobalAggRequest = aggregator.NewAggregator(pre, logger)
	aggregator.GlobalAggRequest.Start(pre, logger)

	aggregator.GlobalAggResponse = aggregator.NewAggregator(pre, logger)
	aggregator.GlobalAggResponse.Start(pre, logger)

	disaggregator.GlobalDisagg = disaggregator.NewDisaggregator(pre, logger)

	server.InitRateLimiter(config.Config_.RateLimit)
	server.InitWorkerPool()

	manager.TunnelMgr = manager.NewTunnelManager(pre, logger)
	manager.NewTunnelStreamSem()
	go func() {
		_ = manager.ListenAndServeQUIC(HandleQUICPacket, pre, logger)
	}()

	for _, port := range config.Config_.Listeners {
		if port.Proto != "tcp" && port.Proto != "udp" {
			continue
		}
		_ = server.ServerMap[port.Proto].Operate.StartServerWithMgr(port.Port, pre, logger)
		go server.ServerMap[port.Proto].Operate.StartServerRun(port.Port, accessLogger, pre, logger)
	}

	router := gin.Default()
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, "success")
	})

	server.ServerManager(router, accessLogger, logger)

	go func() {
		pprofPort := "6060"
		logger.Info("pprof server started", slog.String("pre", pre), "port", pprofPort)
		if err := http.ListenAndServe(":"+pprofPort, nil); err != nil {
			logger.Error("pprof server failed", slog.String("pre", pre), slog.Any("err", err))
		}
	}()

	port := "7083"
	logger.Info("Listening", slog.String("pre", pre), "port", port)
	if err = router.Run(":" + port); err != nil {
		logger.Error("Gin Run failed", slog.String("pre", pre), "err", err)
	}
}
