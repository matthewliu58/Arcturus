package server

import (
	"github.com/gin-gonic/gin"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
)

// 外部声明（由 main.go 提供）
var (
	logger       *slog.Logger
	accessLogger *slog.Logger
)

// 全局端口管理
var (
	portMap     = make(map[int]bool)
	portMutex   sync.RWMutex
	listenerMap = make(map[int]net.Listener)
	listenerMu  sync.RWMutex
)

// TCPServerManager HTTP 接口管理 TCP server
func ServerManager(r *gin.Engine) {
	r.POST("/tcp/start", func(c *gin.Context) {
		portStr := c.Query("port")
		if portStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "port is required"})
			return
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid port"})
			return
		}

		// 检查是否已启动
		portMutex.RLock()
		exists := portMap[port]
		portMutex.RUnlock()

		if exists {
			c.JSON(http.StatusConflict, gin.H{"error": "port already started"})
			return
		}

		// 启动 TCP server（先创建 listener，失败则返回错误）
		err = StartServerWithMgr(port, "server-manager", accessLogger, logger)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// 成功后再用 goroutine 运行
		go StartServerRun(port, "server-manager", accessLogger, logger)

		c.JSON(http.StatusOK, gin.H{"message": "tcp server started", "port": port})
	})

	r.GET("/tcp/list", func(c *gin.Context) {
		portMutex.RLock()
		ports := make([]int, 0, len(portMap))
		for p := range portMap {
			ports = append(ports, p)
		}
		portMutex.RUnlock()
		c.JSON(http.StatusOK, gin.H{"ports": ports})
	})

	r.DELETE("/tcp/stop", func(c *gin.Context) {
		portStr := c.Query("port")
		if portStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "port is required"})
			return
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid port"})
			return
		}

		err = StopServer(port)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "tcp server stopped", "port": port})
	})
}
