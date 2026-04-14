package server

import (
	"data-proxy/util"
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
)

var (
	portMap     = make(map[int]bool)
	portMutex   sync.RWMutex
	listenerMap = make(map[int]interface{})
	listenerMu  sync.RWMutex
)

func ServerManager(r *gin.Engine, a, l *slog.Logger) {
	r.POST("/tcp/start", func(c *gin.Context) {

		req := util.GenerateRandomLetters(5)

		portStr := c.Query("port")
		protocol := c.Query("protocol")
		if portStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "port is required"})
			return
		}
		l.Info("server-manager add", slog.String("req", req),
			slog.String("portStr", portStr), slog.String("protocol", protocol))

		port, err := strconv.Atoi(portStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid port"})
			return
		}

		if protocol != "tcp" && protocol != "udp" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid protocol"})
			return
		}

		portMutex.RLock()
		exists := portMap[port]
		portMutex.RUnlock()

		if exists {
			c.JSON(http.StatusConflict, gin.H{"error": "port already started"})
			return
		}

		err = ServerMap[protocol].Operate.StartServerWithMgr(port, req, l)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// 成功后再用 goroutine 运行
		go ServerMap[protocol].Operate.StartServerRun(port, a, req, l)

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

		req := util.GenerateRandomLetters(5)

		portStr := c.Query("port")
		protocol := c.Query("protocol")
		if portStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "port is required"})
			return
		}
		l.Info("server-manager del", slog.String("req", req),
			slog.String("portStr", portStr), slog.String("protocol", protocol))

		port, err := strconv.Atoi(portStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid port"})
			return
		}

		if protocol != "tcp" && protocol != "udp" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid protocol"})
			return
		}

		err = ServerMap[protocol].Operate.StopServer(port, req, l)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "tcp server stopped", "port": port})
	})
}
