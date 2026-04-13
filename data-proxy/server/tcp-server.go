package server

import (
	"io"
	"log/slog"
	"net"
	"strconv"
	"time"
)

type TCPServer struct {
}

// NewTCPProtocol 创建 TCP 协议实例
func NewTCPServer() *TCPServer {
	return &TCPServer{}
}

// StartTCPServerWithMgr 创建 listener 并注册到管理器（不出错即可返回）
func (t *TCPServer) StartServerWithMgr(port int, pre string, l *slog.Logger) error {
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
func (t *TCPServer) StartServerRun(port int, access *slog.Logger, req string, l *slog.Logger) {
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
func (t *TCPServer) StopServer(port int, req string, l *slog.Logger) error {
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

// directOriginProxy hops <= 2 时，本机直接回源
// 返回是否成功处理（成功返回 true，失败返回 false）
func (t *TCPServer) DirectOriginProxy(conn net.Conn, originAddr string, data []byte, reqID uint32, l *slog.Logger) bool {
	originConn, err := net.DialTimeout("tcp", originAddr, proxyTimeout)
	if err != nil {
		l.Error("dial origin failed", slog.Any("req_id", reqID), slog.Any("err", err))
		return false
	}
	defer originConn.Close()

	// 发给源站
	_, _ = originConn.Write(data)
	// 读回包
	_ = originConn.SetReadDeadline(time.Now().Add(proxyTimeout))
	resp, err := io.ReadAll(originConn)
	if err != nil {
		l.Error("read origin resp failed", slog.Any("req_id", reqID), slog.Any("err", err))
		return false
	}

	// 写回客户端
	_, _ = conn.Write(resp)
	l.Info("direct origin proxy done", slog.Any("req_id", reqID))
	return true
}
