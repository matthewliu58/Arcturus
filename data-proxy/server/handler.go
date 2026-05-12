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
	routingMap   = make(map[int]routingInfo) // TODO: purge not triggered for a long time
	routingMutex sync.RWMutex
)

const (
	routeTimeout = 10 * time.Second
	proxyTimeout = 10 * time.Second
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

var (
	ServerMap = make(map[string]ServerInterface)
)

func InitServerInterface(protocol string, pre string, logger *slog.Logger) ServerInterface {
	switch protocol {
	case "tcp":
		return ServerInterface{Operate: NewTCPServer(true, 0)}
	case "udp":
		return ServerInterface{Operate: NewUDPServer()}
	default:
		return ServerInterface{Operate: nil}
	}
}
