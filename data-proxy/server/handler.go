package server

import (
	"data-proxy/util"
	"log/slog"
	"sync"
	"time"
)

type routingInfo struct {
	info     *util.RoutingInfo
	deadline time.Time
}

var (
	routingMap   = make(map[int]routingInfo) //todo 很久没触发删除
	routingMutex sync.RWMutex
)

const (
	routeTimeout = 30 * time.Second // 路由缓存超时
	proxyTimeout = 5 * time.Second  // 回源 / 等待超时
)

type ServerFuncs interface {
	StartServerWithMgr(port int, pre string, l *slog.Logger) error
	StartServerRun(port int, access *slog.Logger, req string, l *slog.Logger)
	StopServer(port int, req string, l *slog.Logger) error
	//DirectOriginProxy(conn net.Conn, originAddr string, data []byte, reqID uint32, l *slog.Logger) bool
}

type ServerInterface struct {
	Operate ServerFuncs
}

var ServerHandler ServerInterface

func InitServerInterface(protocol string, pre string, logger *slog.Logger) ServerInterface {
	switch protocol {
	case "tcp":
		return ServerInterface{Operate: NewTCPServer()}
	default:
		return ServerInterface{Operate: nil}
	}
}
