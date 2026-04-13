package server

import (
	"data-proxy/aggregator"
	"data-proxy/disaggregator"
	"data-proxy/util"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

type UDPServer struct{}

func NewUDPServer() *UDPServer {
	return &UDPServer{}
}

// StartServerWithMgr 初始化 UDP 监听并注册
func (u *UDPServer) StartServerWithMgr(port int, pre string, l *slog.Logger) error {
	addr := &net.UDPAddr{Port: port}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}

	listenerMu.Lock()
	listenerMap[port] = conn
	portMutex.Lock()
	portMap[port] = true
	portMutex.Unlock()
	listenerMu.Unlock()

	l.Info("udp server started",
		slog.String("pre", pre),
		slog.Int("port", port),
	)
	return nil
}

// StartServerRun 启动 UDP 接收循环（阻塞）
func (u *UDPServer) StartServerRun(port int, accessLogger *slog.Logger, req string, logger *slog.Logger) {
	listenerMu.RLock()
	conn, ok := listenerMap[port].(*net.UDPConn)
	listenerMu.RUnlock()

	if !ok || conn == nil {
		logger.Error("udp listener not found", slog.Int("port", port))
		return
	}

	buf := make([]byte, 65535) // UDP 最大包

	for {
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			logger.Error("udp read failed", slog.Any("err", err))
			continue
		}

		// 必须异步，否则 UDP 会丢包
		go func() {
			data := make([]byte, n)
			copy(data, buf[:n])
			handleUDPConnection(conn, clientAddr, port, data, accessLogger, logger)
		}()
	}
}

// StopServer 关闭 UDP
func (u *UDPServer) StopServer(port int, req string, l *slog.Logger) error {
	listenerMu.Lock()
	defer listenerMu.Unlock()

	conn, ok := listenerMap[port].(*net.UDPConn)
	if !ok {
		return nil
	}

	err := conn.Close()
	delete(listenerMap, port)

	portMutex.Lock()
	delete(portMap, port)
	portMutex.Unlock()

	l.Info("udp server stopped", slog.Int("port", port))
	return err
}

// DirectOriginProxy UDP 直接回源
func (u *UDPServer) DirectOriginProxy(
	conn *net.UDPConn,
	clientAddr *net.UDPAddr,
	originAddr string,
	data []byte,
	reqID uint32,
	l *slog.Logger,
) bool {
	originConn, err := net.DialTimeout("udp", originAddr, proxyTimeout)
	if err != nil {
		l.Error("udp dial origin failed",
			slog.Any("req_id", reqID),
			slog.Any("err", err),
		)
		return false
	}
	defer originConn.Close()

	_, _ = originConn.Write(data)

	_ = originConn.SetReadDeadline(time.Now().Add(proxyTimeout))
	respBuf := make([]byte, 65535)
	n, err := originConn.Read(respBuf)
	if err != nil {
		l.Error("udp read origin failed",
			slog.Any("req_id", reqID),
			slog.Any("err", err),
		)
		return false
	}

	_, err = conn.WriteToUDP(respBuf[:n], clientAddr)
	if err != nil {
		l.Error("udp write response failed", slog.Any("req_id", reqID))
		return false
	}

	l.Info("udp direct proxy done", slog.Any("req_id", reqID))
	return true
}

// ------------------------------
// UDP 业务处理（和 TCP 完全对齐）
// ------------------------------
func handleUDPConnection(
	conn *net.UDPConn,
	clientAddr *net.UDPAddr,
	port int,
	data []byte,
	accessLogger *slog.Logger,
	logger *slog.Logger,
) {
	clientIP := clientAddr.IP.String()

	// 限流
	if globalRL != nil && !globalRL.Allow(port, clientIP) {
		logger.Warn("udp rate limited",
			slog.String("client_ip", clientIP),
			slog.Int("port", port),
		)
		return
	}

	reqID := util.GenShortReqID(clientIP)
	start := time.Now()

	// 访问日志
	rtMs := float64(time.Since(start).Microseconds()) / 1000
	accessLogger.Info("udp access",
		slog.Any("req_id", reqID),
		slog.String("client_ip", clientIP),
		slog.Float64("rt_ms", rtMs),
		slog.Int("data_len", len(data)),
	)

	// 路由
	routingMutex.RLock()
	ri, hasRoute := routingMap[port]
	routingMutex.RUnlock()

	if !hasRoute || time.Now().After(ri.deadline) || ri.info == nil {
		logger.Error("udp no valid route", slog.Any("req_id", reqID))
		return
	}

	routeInfo := ri.info
	if len(routeInfo.Routing) == 0 {
		logger.Error("udp routing empty", slog.Any("req_id", reqID))
		return
	}
	pathInfo := routeInfo.Routing[0]

	// 直接回源
	if len(pathInfo.Hops) <= 2 {
		originAddr := pathInfo.Hops[len(pathInfo.Hops)-1]
		if ok := NewUDPServer().DirectOriginProxy(
			conn, clientAddr, originAddr, data, reqID, logger,
		); ok {
			return
		}
	}

	// 走聚合隧道
	userID := util.GenShortReqID(clientIP)
	nextHop := util.HopIPToNet(pathInfo.Hops[1])

	var routingKey, portStr string
	for _, h := range pathInfo.Hops {
		if idx := strings.Index(h, ":"); idx != -1 {
			portStr = h[idx+1:]
		}
		routingKey += "," + h
	}

	p64, _ := strconv.ParseUint(portStr, 10, 16)

	// 注册等待通道
	waitCh, cleanup := disaggregator.GlobalDisagg.Register(userID)
	defer cleanup()

	// 入队
	aggregator.GlobalAggRequest.AddToBatch(
		true, // UDP=true
		routingKey,
		uint16(p64),
		pathInfo,
		nextHop,
		userID,
		data,
	)

	// 等待回包
	select {
	case respData, ok := <-waitCh:
		if !ok {
			logger.Error("udp chan closed", slog.Any("req_id", reqID))
			return
		}
		_, _ = conn.WriteToUDP(respData, clientAddr)
		logger.Info("udp proxy response sent", slog.Any("req_id", reqID))

	case <-time.After(proxyTimeout):
		logger.Error("udp response timeout", slog.Any("req_id", reqID))
	}
}
