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
	"sync"
	"time"
)

type TCPServer struct {
	protocol string
}

var (
	accessLogMap sync.Map
	accessWindow = 5 * time.Second
)

func shouldLogAccess(clientIP string) bool {
	now := time.Now()

	lastTimeVal, exists := accessLogMap.Load(clientIP)
	if !exists {
		accessLogMap.Store(clientIP, now)
		return true
	}

	lastTime := lastTimeVal.(time.Time)
	if now.Sub(lastTime) >= accessWindow {
		accessLogMap.Store(clientIP, now)
		return true
	}

	return false
}

func NewTCPServer() *TCPServer {
	return &TCPServer{
		protocol: "tcp",
	}
}

func (t *TCPServer) StartServerWithMgr(port int, pre string, l *slog.Logger) error {
	port_ := ":" + strconv.Itoa(port)
	listener, err := net.Listen("tcp", port_)
	if err != nil {
		return err
	}

	listenerMu.Lock()
	listenerMap[port] = listener
	portMutex.Lock()
	portMap[port] = true
	portMutex.Unlock()
	listenerMu.Unlock()

	l.Info("server started success", slog.String("pre", pre),
		slog.String("protocol", t.protocol), slog.String("port", port_))
	return nil
}

func (t *TCPServer) StartServerRun(port int, access *slog.Logger, req string, l *slog.Logger) {
	listenerMu.RLock()
	listener_, _ := listenerMap[port]
	listenerMu.RUnlock()
	listener := listener_.(net.Listener)

	for {
		conn, err := listener.Accept()
		if err != nil {
			l.Error("accept failed", slog.String("req", req), slog.Any("err", err))
			continue
		}
		go handleConnection(conn, port, access, l)
	}
}

func (t *TCPServer) StopServer(port int, req string, l *slog.Logger) error {
	listenerMu.Lock()
	defer listenerMu.Unlock()

	l.Info("stop server", slog.String("req", req),
		slog.String("protocol", t.protocol), slog.Int("port", port))

	listener_, ok := listenerMap[port]
	if !ok {
		return nil
	}
	listener := listener_.(net.Listener)

	err := listener.Close()
	delete(listenerMap, port)
	portMutex.Lock()
	delete(portMap, port)
	portMutex.Unlock()

	return err
}

func directOriginProxy(conn net.Conn, originAddr string, data []byte, reqID uint32, l *slog.Logger) bool {
	originConn, err := net.DialTimeout("tcp", originAddr, proxyTimeout)
	if err != nil {
		l.Error("dial origin failed", slog.Any("req_id", reqID),
			slog.String("originAddr", originAddr), slog.Any("err", err))
		return false
	}
	defer originConn.Close()

	_, _ = originConn.Write(data)

	_ = originConn.SetReadDeadline(time.Now().Add(proxyTimeout))
	resp, err := io.ReadAll(originConn)
	if err != nil {
		l.Error("read origin resp failed", slog.Any("req_id", reqID),
			slog.String("originAddr", originAddr), slog.Any("err", err))
		return false
	}

	_, _ = conn.Write(resp)
	l.Info("direct origin proxy done", slog.Any("req_id", reqID), slog.String("originAddr", originAddr))
	return true
}

func handleConnection(conn net.Conn, port int, a, l *slog.Logger) {
	defer func() { _ = conn.Close() }()

	clientAddr := conn.RemoteAddr().(*net.TCPAddr)
	clientIP := clientAddr.IP.String()

	if globalRL != nil && !globalRL.Allow(port, clientIP) {
		l.Warn("rate limit exceeded", slog.String("client_ip", clientIP), slog.Int("port", port))
		return
	}

	reqID := util.GenShortReqID(clientIP)
	l.Info("new connection", slog.String("client_ip", clientIP),
		slog.String("protocol", "tcp"), slog.Int("port", port), slog.Any("req_id", reqID))

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	start := time.Now()
	data, err := io.ReadAll(conn)
	rtMs := float64(time.Since(start).Microseconds()) / 1000

	if err != nil {
		l.Error("", slog.Any("req_id", reqID), slog.String("client_ip", clientIP), slog.Any("err", err))
		return
	}

	go func() {
		if shouldLogAccess(clientIP) {
			a.Info("access", slog.Any("req_id", reqID), slog.String("client_ip", clientIP),
				slog.Float64("conn_rt_ms", rtMs), slog.Int("data_len", len(data)))
		}
	}()

	routingMutex.RLock()
	ri, hasRoute := routingMap[port]
	routingMutex.RUnlock()

	var routeInfo *util.RoutingInfo
	if !hasRoute {
		routeInfo = GetRoutingFromControlPlane(port, l)

		routingMutex.Lock()
		routingMap[port] = routingInfo{
			info:     routeInfo,
			deadline: time.Now().Add(routeTimeout),
		}
		routingMutex.Unlock()
	} else if time.Now().After(ri.deadline) {
		routeInfo = ri.info
		go func() {
			routeInfo = GetRoutingFromControlPlane(port, l)

			routingMutex.Lock()
			routingMap[port] = routingInfo{
				info:     routeInfo,
				deadline: time.Now().Add(routeTimeout),
			}
			routingMutex.Unlock()
		}()
	} else {
		routeInfo = ri.info
	}

	if len(routeInfo.Routing) == 0 {
		l.Error("no path in routing info", slog.Any("req_id", reqID))
		return
	}
	pathInfo := routeInfo.Routing[0]

	if len(pathInfo.Hops) <= 2 {
		originAddr := pathInfo.Hops[len(pathInfo.Hops)-1]
		if ok := directOriginProxy(conn, originAddr, data, reqID, l); !ok {
			l.Error("direct origin proxy failed", slog.Any("req_id", reqID))
		}
		return
	}

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

	waitCh, cleanup := disaggregator.GlobalDisagg.Register(userID)
	defer cleanup()

	aggregator.GlobalAggRequest.AddToBatch(false, routingKey, uint16(p64), pathInfo, nextHop, userID, data)

	select {
	case respData, ok := <-waitCh:
		if !ok {
			l.Error("wait chan closed", slog.Any("req_id", reqID))
			return
		}
		_, _ = conn.Write(respData)
		l.Info("aggregated proxy response sent", slog.Any("req_id", reqID))

	case <-time.After(proxyTimeout):
		l.Error("wait response timeout", slog.Any("req_id", reqID))
	}
}
