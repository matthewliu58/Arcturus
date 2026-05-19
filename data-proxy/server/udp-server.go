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

const udpConcurrentLimit = 16

var udpSem = make(chan struct{}, udpConcurrentLimit)

type UDPServer struct {
	protocol string
}

func NewUDPServer() *UDPServer {
	return &UDPServer{
		protocol: "udp",
	}
}

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

	l.Info("udp server started", slog.String("pre", pre),
		slog.String("protocol", u.protocol), slog.Int("port", port))
	return nil
}

func (u *UDPServer) StartServerRun(port int, accessLogger *slog.Logger, req string, logger *slog.Logger) {
	listenerMu.RLock()
	conn, ok := listenerMap[port].(*net.UDPConn)
	listenerMu.RUnlock()

	if !ok || conn == nil {
		logger.Error("udp listener not found", slog.String("req", req),
			slog.String("protocol", u.protocol), slog.Int("port", port))
		return
	}

	buf := make([]byte, 65535)

	for {
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			logger.Error("udp read failed", slog.String("req", req), slog.Any("err", err))
			continue
		}

		go func() {
			udpSem <- struct{}{}
			defer func() { <-udpSem }()

			data := make([]byte, n)
			copy(data, buf[:n])
			handleUDPConnection(conn, clientAddr, port, data, accessLogger, logger)
		}()
	}
}

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

	l.Info("udp server stopped", slog.String("req", req),
		slog.String("protocol", u.protocol), slog.Int("port", port))
	return err
}

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

func handleUDPConnection(
	conn *net.UDPConn,
	clientAddr *net.UDPAddr,
	port int,
	data []byte,
	accessLogger *slog.Logger,
	logger *slog.Logger,
) {
	clientIP := clientAddr.IP.String()

	if globalRL != nil && !globalRL.Allow(port, clientIP) {
		logger.Warn("udp rate limited",
			slog.String("client_ip", clientIP),
			slog.Int("port", port),
		)
		return
	}

	reqID := util.GenShortReqID(clientIP)

	logger.Info("udp request received", slog.Any("req_id", reqID),
		slog.Any("clientAddr", clientAddr), slog.Int("port", port))

	routingMutex.RLock()
	ri, hasRoute := routingMap[port]
	routingMutex.RUnlock()

	var routeInfo *util.RoutingInfo
	if !hasRoute {
		routeInfo = GetRoutingFromControlPlane(port, logger)

		routingMutex.Lock()
		routingMap[port] = routingInfo{
			info:     routeInfo,
			deadline: time.Now().Add(routeTimeout),
		}
		routingMutex.Unlock()
	} else if time.Now().After(ri.deadline) {
		routeInfo = ri.info
		go func() {
			newRouteInfo := GetRoutingFromControlPlane(port, logger)
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
		logger.Error("udp routing empty", slog.Any("req_id", reqID))
		return
	}
	pathInfo := routeInfo.Routing[0]

	if len(pathInfo.Hops) <= 2 {
		originAddr := pathInfo.Hops[len(pathInfo.Hops)-1]
		if ok := NewUDPServer().DirectOriginProxy(
			conn, clientAddr, originAddr, data, reqID, logger,
		); !ok {
			logger.Error("udp direct proxy failed", slog.Any("req_id", reqID))
		}
		return
	}

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
	pathInfo_ := util.PathInfo{Hops: hops}
	routingKey := strings.Join(hops, ",")
	p64, _ := strconv.ParseUint(port_, 10, 16)

	waitCh, cleanup := disaggregator.GlobalDisagg.Register(reqID)
	defer cleanup()

	aggregator.GlobalAggRequest.AddToBatch(
		true, // UDP=true
		routingKey,
		"udp",
		uint16(p64),
		pathInfo_,
		nextHop,
		reqID,
		data,
	)

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
