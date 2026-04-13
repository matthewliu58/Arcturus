package server

import (
	"data-proxy/aggregator"
	"data-proxy/disaggregator"
	"data-proxy/util"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
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
	listener_, _ := listenerMap[port]
	listenerMu.RUnlock()
	listener := listener_.(net.Listener)

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

	listener_, ok := listenerMap[port]
	if !ok {
		return nil // 已关闭
	}
	listener := listener_.(net.Listener)

	err := listener.Close()
	delete(listenerMap, port)
	portMutex.Lock()
	delete(portMap, port)
	portMutex.Unlock()

	return err
}

// directOriginProxy hops <= 2 时，本机直接回源
// 返回是否成功处理（成功返回 true，失败返回 false）
func directOriginProxy(conn net.Conn, originAddr string, data []byte, reqID uint32, l *slog.Logger) bool {
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

// handleConnection 不改动
func handleConnection(conn net.Conn, port int, a, l *slog.Logger) {
	defer func() { _ = conn.Close() }()

	clientAddr := conn.RemoteAddr().(*net.TCPAddr)
	clientIP := clientAddr.IP.String()

	// 限流检查
	if globalRL != nil && !globalRL.Allow(port, clientIP) {
		l.Warn("rate limit exceeded", slog.String("client_ip", clientIP), slog.Int("port", port))
		return
	}

	reqID := util.GenShortReqID(clientIP)
	start := time.Now()

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	data, err := io.ReadAll(conn)
	rtMs := float64(time.Since(start).Microseconds()) / 1000

	if err != nil {
		l.Error("", slog.Any("req_id", reqID), slog.String("client_ip", clientIP), slog.Any("err", err))
		return
	}

	a.Info("access", slog.Any("req_id", reqID), slog.String("client_ip", clientIP),
		slog.Float64("conn_rt_ms", rtMs), slog.Int("data_len", len(data)))

	routingMutex.RLock()
	ri, hasRoute := routingMap[port]
	routingMutex.RUnlock()

	var routeInfo *util.RoutingInfo
	if !hasRoute || time.Now().After(ri.deadline) {
		go func() {
			//todo get routing from control plane by an goroutine
			routeInfo = &util.RoutingInfo{} //prot

			// 更新缓存
			routingMutex.Lock()
			routingMap[port] = routingInfo{
				info:     routeInfo,
				deadline: time.Now().Add(routeTimeout),
			}
			routingMutex.Unlock()
		}()
	}
	routeInfo = ri.info

	if len(routeInfo.Routing) == 0 {
		l.Error("no path in routing info", slog.Any("req_id", reqID))
		return
	}
	pathInfo := routeInfo.Routing[0]

	// 2. hops <= 2：本机直接回源
	if len(pathInfo.Hops) <= 2 {
		originAddr := pathInfo.Hops[len(pathInfo.Hops)-1]
		if ok := directOriginProxy(conn, originAddr, data, reqID, l); ok {
			return
		}
	}

	// 3. 走聚合隧道：注册等待 chan 并阻塞
	userID := util.GenShortReqID(clientIP)
	nextHop := util.HopIPToNet(pathInfo.Hops[1])

	routingKey := ""
	port_ := ""
	for _, h := range pathInfo.Hops {
		if strings.Contains(h, ":") {
			h = h[strings.Index(h, ":"):]
			port_ = h[:strings.Index(h, ":")]
		}
		routingKey = routingKey + "," + h
	}
	p64, _ := strconv.ParseUint(port_, 10, 16)

	// 注册等待通道：本协程创建的 chan
	waitCh, cleanup := disaggregator.GlobalDisagg.Register(userID)
	defer cleanup() // 函数退出自动注销

	// 扔进聚合器
	aggregator.GlobalAggRequest.AddToBatch(false, routingKey, uint16(p64), pathInfo, nextHop, userID, data)

	// 阻塞等下行回复
	select {
	case respData, ok := <-waitCh:
		if !ok {
			l.Error("wait chan closed", slog.Any("req_id", reqID))
			return
		}
		// 写回客户端
		_, _ = conn.Write(respData)
		l.Info("aggregated proxy response sent", slog.Any("req_id", reqID))

	case <-time.After(proxyTimeout):
		l.Error("wait response timeout", slog.Any("req_id", reqID))
	}
}
