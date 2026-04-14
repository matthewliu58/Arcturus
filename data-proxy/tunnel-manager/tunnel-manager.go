package tunnel_manager

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"sync"

	"github.com/quic-go/quic-go"
)

var (
	TunnelMgr *TunnelManager
)

const (
	QUIC_PORT = "4433"
)

type TunnelManager struct {
	mu      sync.RWMutex
	tunnels map[string]*quic.Conn // TODO: purge not triggered for a long time
}

func NewTunnelManager(pre string, l *slog.Logger) *TunnelManager {
	l.Info("QUIC manager started", slog.String("pre", pre))
	return &TunnelManager{
		tunnels: make(map[string]*quic.Conn),
	}
}

func (m *TunnelManager) SendPacket(
	ctx context.Context,
	remoteIP net.IP,
	//pkt *tunnel_packet.Packet,
	data []byte, pre string, l *slog.Logger,
) error {
	if remoteIP == nil {
		return errors.New("remote ip is nil")
	}

	//pkt.SerializeHead()
	//data := pkt.Buf[:pkt.TotalBytes()]

	conn, err := m.GetOrCreateTunnel(ctx, remoteIP, pre, l)
	if err != nil {
		return err
	}

	success := true
	stream, err := conn.OpenUniStreamSync(ctx)
	if err != nil {
		m.CloseTunnel(remoteIP, pre, l)
		success = false
	}

	if !success {
		conn, err = m.GetOrCreateTunnel(ctx, remoteIP, pre, l)
		if err != nil {
			return err
		}
		stream, err = conn.OpenUniStreamSync(ctx)
		if err != nil {
			return err
		}
	}
	defer stream.Close()

	_, err = stream.Write(data)
	if err != nil {
		m.CloseTunnel(remoteIP, pre, l)
	}
	return err
}

func (m *TunnelManager) GetOrCreateTunnel(
	ctx context.Context, remoteIP net.IP, pre string, l *slog.Logger) (*quic.Conn, error) {

	addr := net.JoinHostPort(remoteIP.String(), QUIC_PORT)

	m.mu.RLock()
	conn, ok := m.tunnels[addr]
	m.mu.RUnlock()
	if ok {
		return conn, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if conn, ok = m.tunnels[addr]; ok {
		return conn, nil
	}

	tlsCfg := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"tunnel-quic"},
	}

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}

	conn, err = quic.Dial(ctx, udpConn, udpAddr, tlsCfg, &quic.Config{})
	if err != nil {
		return nil, err
	}

	m.tunnels[addr] = conn
	l.Info("QUIC tunnel established", slog.String("pre", pre), slog.String("addr", addr))
	return conn, nil
}

func (m *TunnelManager) CloseTunnel(remoteIP net.IP, pre string, l *slog.Logger) {
	addr := net.JoinHostPort(remoteIP.String(), QUIC_PORT)

	m.mu.Lock()
	defer m.mu.Unlock()

	if conn, ok := m.tunnels[addr]; ok {
		_ = conn.CloseWithError(0, "connection failed")
		delete(m.tunnels, addr)
		l.Info("QUIC tunnel closed", slog.String("pre", pre), slog.String("addr", addr))
	}
}
