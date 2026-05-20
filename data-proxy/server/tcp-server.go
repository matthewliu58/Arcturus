package server

import (
	"bufio"
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
	protocol      string
	keepAlive     bool
	keepAliveTime time.Duration
}

var (
	accessLogMap         sync.Map
	accessWindow         = 5 * time.Second
	defaultKeepAliveTime = 30 * time.Second

	tcpConcurrentLimit = 16
	tcpSem             = make(chan struct{}, tcpConcurrentLimit)
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

func NewTCPServer(keepAlive bool, keepAliveTime time.Duration) *TCPServer {
	if keepAliveTime == 0 {
		keepAliveTime = defaultKeepAliveTime
	}
	return &TCPServer{
		protocol:      "tcp",
		keepAlive:     keepAlive,
		keepAliveTime: keepAliveTime,
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
		if t.keepAlive {
			tcpSem <- struct{}{}
			go func(c net.Conn) {
				defer func() { <-tcpSem }()
				handleConnectionKeepAlive(c, port, access, l, t)
			}(conn)
		} else {
			tcpSem <- struct{}{}
			go func(c net.Conn) {
				defer func() { <-tcpSem }()
				handleConnection(c, port, access, l)
			}(conn)
		}
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
		l.Error("dial origin failed", slog.Any("reqId", reqID),
			slog.String("originAddr", originAddr), slog.Any("err", err))
		return false
	}
	defer originConn.Close()

	_, _ = originConn.Write(data)

	_ = originConn.SetReadDeadline(time.Now().Add(proxyTimeout))
	resp, err := io.ReadAll(originConn)
	if err != nil {
		l.Error("read origin resp failed", slog.Any("reqId", reqID),
			slog.String("originAddr", originAddr), slog.Any("err", err))
		return false
	}

	_, _ = conn.Write(resp)
	l.Info("direct origin proxy done", slog.Any("reqId", reqID), slog.String("originAddr", originAddr))
	return true
}

func handleConnection(conn net.Conn, port int, a, l *slog.Logger) {
	defer func() { _ = conn.Close() }()

	clientAddr := conn.RemoteAddr().(*net.TCPAddr)
	clientIP := clientAddr.IP.String()

	//todo recover rate limiter, shut down it temporarily for benchmark
	//if globalRL != nil && !globalRL.Allow(port, clientIP) {
	//	l.Warn("rate limit exceeded", slog.String("clientIp", clientIP), slog.Int("port", port))
	//	return
	//}

	reqID := util.GenShortReqID(clientIP)
	l.Info("new connection", slog.String("clientIp", clientIP),
		slog.String("protocol", "tcp"), slog.Int("port", port), slog.Any("reqId", reqID))

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	start := time.Now()
	data, err := io.ReadAll(conn)
	rtMs := float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		l.Error("read content failed", slog.Any("reqId", reqID),
			slog.String("clientIp", clientIP), slog.Any("err", err))
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
		if routeInfo != nil {
			routingMutex.Lock()
			routingMap[port] = routingInfo{
				info:     routeInfo,
				deadline: time.Now().Add(routeTimeout),
			}
			routingMutex.Unlock()
		}
	} else if time.Now().After(ri.deadline) {
		routeInfo = ri.info
		go func() {
			newRouteInfo := GetRoutingFromControlPlane(port, l)
			if newRouteInfo != nil {
				routingMutex.Lock()
				routingMap[port] = routingInfo{
					info:     newRouteInfo,
					deadline: time.Now().Add(routeTimeout),
				}
				routingMutex.Unlock()
			}
		}()
	} else {
		routeInfo = ri.info
	}

	if len(routeInfo.Routing) == 0 {
		l.Error("no path in routing info", slog.Any("reqId", reqID))
		return
	}
	pathInfo := routeInfo.Routing[0]

	if len(pathInfo.Hops) <= 2 {
		originAddr := pathInfo.Hops[len(pathInfo.Hops)-1]
		if ok := directOriginProxy(conn, originAddr, data, reqID, l); !ok {
			l.Error("direct origin proxy failed", slog.Any("reqId", reqID))
		}
		return
	}

	//userID := reqID
	nextHop := util.HopIPToNet(pathInfo.Hops[1])

	var hops []string
	port_ := ""
	for _, h := range pathInfo.Hops {
		if strings.Contains(h, ":") {
			t := strings.Split(h, ":")
			h = t[0]
			port_ = t[1]
		}
		hops = append(hops, h)
	}
	routingKey := strings.Join(hops, ",")
	p64, _ := strconv.ParseUint(port_, 10, 16)

	waitCh, cleanup := disaggregator.GlobalDisagg.Register(reqID)
	defer cleanup()

	pathInfo_ := util.PathInfo{Hops: hops}
	aggregator.GlobalAggRequest.AddToBatch(false, routingKey, "tcp", uint16(p64), pathInfo_, nextHop, reqID, data)

	select {
	case respData, ok := <-waitCh:
		if !ok {
			l.Error("wait chan closed", slog.Any("reqId", reqID))
			return
		}
		_, _ = conn.Write(respData)
		l.Info("aggregated proxy response sent", slog.Any("reqId", reqID))

	case <-time.After(proxyTimeout):
		l.Error("wait response timeout", slog.Any("reqId", reqID))
	}
}

func handleConnectionKeepAlive(conn net.Conn, port int, a, l *slog.Logger, server *TCPServer) {
	defer func() { _ = conn.Close() }()

	clientAddr := conn.RemoteAddr().(*net.TCPAddr)
	clientIP := clientAddr.IP.String()

	//todo recover rate limiter, shut down it temporarily for benchmark
	//if globalRL != nil && !globalRL.Allow(port, clientIP) {
	//	l.Warn("rate limit exceeded", slog.String("client_ip", clientIP), slog.Int("port", port))
	//	return
	//}

	reqID := util.GenShortReqID(clientIP)
	l.Info("new connection (keep-alive)", slog.Any("reqId", reqID), slog.String("clientIp", clientIP),
		slog.String("protocol", "tcp"), slog.Int("port", port), slog.Duration("keepAliveTime", server.keepAliveTime))

	connStart := time.Now()
	connDeadline := connStart.Add(server.keepAliveTime)

	for {
		_ = conn.SetReadDeadline(connDeadline)
		start := time.Now()
		//data, err := io.ReadAll(conn)

		reader := bufio.NewReader(conn)
		data, err := reader.ReadBytes('\n')

		rtMs := float64(time.Since(start).Microseconds()) / 1000

		if err != nil {
			if err == io.EOF {
				l.Info("client closed connection", slog.Any("reqId", reqID),
					slog.String("clientIp", clientIP), slog.Duration("connDuration", time.Since(connStart)))
			} else {
				l.Error("read content failed", slog.Any("reqId", reqID),
					slog.String("clientIp", clientIP), slog.Any("err", err))
			}
			return
		}

		if len(data) == 0 {
			l.Info("client sent empty request, closing connection", slog.Any("reqId", reqID),
				slog.String("clientIp", clientIP))
			return
		} else {
			l.Debug("client sent request", slog.Any("reqId", reqID), slog.String("data", string(data)))
		}

		go func(reqData []byte, currentReqID uint32) {
			if shouldLogAccess(clientIP) {
				a.Info("access", slog.Any("req_id", currentReqID), slog.String("client_ip", clientIP),
					slog.Float64("conn_rt_ms", rtMs), slog.Int("data_len", len(reqData)))
			}
		}(data, reqID)

		routingMutex.RLock()
		ri, hasRoute := routingMap[port]
		routingMutex.RUnlock()

		var routeInfo *util.RoutingInfo
		if !hasRoute {
			routeInfo = GetRoutingFromControlPlane(port, l)
			if routeInfo != nil {
				routingMutex.Lock()
				routingMap[port] = routingInfo{
					info:     routeInfo,
					deadline: time.Now().Add(routeTimeout),
				}
				routingMutex.Unlock()
			}
		} else if time.Now().After(ri.deadline) {
			routeInfo = ri.info
			go func() {
				newRouteInfo := GetRoutingFromControlPlane(port, l)
				if newRouteInfo != nil {
					routingMutex.Lock()
					routingMap[port] = routingInfo{
						info:     newRouteInfo,
						deadline: time.Now().Add(routeTimeout),
					}
					routingMutex.Unlock()
				}
			}()
		} else {
			routeInfo = ri.info
		}

		if len(routeInfo.Routing) == 0 {
			l.Error("no path in routing info", slog.Any("reqId", reqID))
			return
		}
		pathInfo := routeInfo.Routing[0]

		if len(pathInfo.Hops) <= 2 {
			originAddr := pathInfo.Hops[len(pathInfo.Hops)-1]
			if ok := directOriginProxy(conn, originAddr, data, reqID, l); !ok {
				l.Error("direct origin proxy failed", slog.Any("reqId", reqID))
			}
		} else {
			//userID := util.GenShortReqID(clientIP)
			nextHop := util.HopIPToNet(pathInfo.Hops[1])

			var hops []string
			port_ := ""
			for _, h := range pathInfo.Hops {
				if strings.Contains(h, ":") {
					t := strings.Split(h, ":")
					h = t[0]
					port_ = t[1]
				}
				hops = append(hops, h)
			}
			routingKey := strings.Join(hops, ",")
			p64, _ := strconv.ParseUint(port_, 10, 16)

			waitCh, cleanup := disaggregator.GlobalDisagg.Register(reqID)

			pathInfo_ := util.PathInfo{Hops: hops}
			aggregator.GlobalAggRequest.AddToBatch(false, routingKey, "tcp", uint16(p64), pathInfo_, nextHop, reqID, data)

			select {
			case respData, ok := <-waitCh:
				cleanup()
				if !ok {
					l.Error("wait chan closed", slog.Any("reqId", reqID))
					return
				}
				_ = conn.SetWriteDeadline(time.Now().Add(proxyTimeout))
				l.Debug("client receive response", slog.Any("reqId", reqID), slog.String("respData", string(respData)))
				_, _ = conn.Write(respData)
				l.Info("aggregated proxy response sent", slog.Any("reqId", reqID))

			case <-time.After(proxyTimeout):
				cleanup()
				l.Error("wait response timeout", slog.Any("reqId", reqID))
			}
		}

		reqID = util.GenShortReqID(clientIP)

		if time.Now().After(connDeadline) {
			l.Info("connection keep alive timeout", slog.Any("reqId", reqID),
				slog.String("clientIp", clientIP), slog.Duration("connDuration", time.Since(connStart)))
			return
		}
	}
}
