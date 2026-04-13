package server

import (
	"log/slog"
	"net"
	"strconv"
)

// StartTCPServerWithMgr 创建 listener 并注册到管理器（不出错即可返回）
func StartServerWithMgr(port int, pre string, access, l *slog.Logger) error {
	port_ := ":" + strconv.Itoa(port)
	listener, err := net.Listen("tcp", port_)
	if err != nil {
		return err
	}

	// 记录 listener
	listenerMu.Lock()
	listenerMap[port] = listener
	portMutex.Lock()
	portMap[port] = true
	portMutex.Unlock()
	listenerMu.Unlock()

	l.Info("server started success", slog.String("pre", pre), slog.String("port", port_))
	return nil
}

// StartTCPServerRun 从 map 获取 listener，开始 accept 循环（阻塞）
func StartServerRun(port int, pre string, access, l *slog.Logger) {
	listenerMu.RLock()
	listener, _ := listenerMap[port]
	listenerMu.RUnlock()

	for {
		conn, err := listener.Accept()
		if err != nil {
			l.Error("accept failed", slog.Any("err", err))
			continue
		}
		go handleConnection(conn, port, access, l)
	}
}

// StopTCPServer 停止指定端口的 TCP server
func StopServer(port int) error {
	listenerMu.Lock()
	defer listenerMu.Unlock()

	listener, ok := listenerMap[port]
	if !ok {
		return nil // 已关闭
	}

	err := listener.Close()
	delete(listenerMap, port)
	portMutex.Lock()
	delete(portMap, port)
	portMutex.Unlock()

	return err
}
