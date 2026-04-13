package server

import (
	"log/slog"
	"net"
)

type ServerFuncs interface {
	StartServerWithMgr(port int, pre string, l *slog.Logger) error
	StartServerRun(port int, access *slog.Logger, req string, l *slog.Logger)
	StopServer(port int, req string, l *slog.Logger) error
	DirectOriginProxy(conn net.Conn, originAddr string, data []byte, reqID uint32, l *slog.Logger) bool
}

type ServerInterface struct {
	Operate ServerFuncs
}

var ServerHandler ServerInterface

func InitServerInterface(protocol string, pre string, logger *slog.Logger) ServerInterface {
	switch protocol {
	case "TCP":
		return ServerInterface{Operate: NewTCPServer()}
	default:
		return ServerInterface{Operate: nil}
	}
}
