package main

import (
	"context"
	"data-proxy/config"
	"data-proxy/util"
	"github.com/gin-gonic/gin"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"log/slog"
)

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

	// 启动 TCP server
	go StartTCPServer(pre, accessLogger, logger)

	// Gin
	router := gin.Default()
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, "success")
	})

	port := "7095"
	logger.Info("Listening", slog.String("pre", pre), "port", port)
	if err := router.Run(":" + port); err != nil {
		logger.Error("Gin Run failed", slog.String("pre", pre), "err", err)
	}
}

// StartTCPServer 不改动
func StartTCPServer(pre string, access, logger *slog.Logger) error {
	listener, err := net.Listen("tcp", ":7096")
	if err != nil {
		return err
	}
	defer listener.Close()

	logger.Info("tcp server started success", slog.String("pre", pre), slog.String("port", "7096"))

	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Error("accept failed", slog.Any("err", err))
			continue
		}
		go handleConnection(conn, access, logger)
	}
}

// handleConnection 不改动
func handleConnection(conn net.Conn, a, l *slog.Logger) {
	defer func() { _ = conn.Close() }()

	clientAddr := conn.RemoteAddr().(*net.TCPAddr)
	clientIP := clientAddr.IP.String()
	reqID := util.GenShortReqID(clientIP)
	start := time.Now()

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	data, err := io.ReadAll(conn)
	rtMs := float64(time.Since(start).Microseconds()) / 1000

	if err != nil {
		l.Error("", slog.String("req_id", reqID), slog.String("client_ip", clientIP), slog.Any("err", err))
		return
	}

	a.Info("access", slog.String("req_id", reqID), slog.String("client_ip", clientIP),
		slog.Float64("conn_rt_ms", rtMs), slog.Int("data_len", len(data)))

	// 你的业务逻辑在这里
}
