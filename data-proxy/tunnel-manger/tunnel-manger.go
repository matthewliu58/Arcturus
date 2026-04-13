package tunnel_packet

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log"
	"math/big"
	"net"
	"sync"

	"data-proxy/tunnel-packet"
	"github.com/quic-go/quic-go"
)

var (
	TunnelMgr = NewTunnelManager()
)

const (
	QUIC_PORT = "4433"
)

// TunnelManager 管理所有 QUIC 隧道
type TunnelManager struct {
	mu      sync.RWMutex
	tunnels map[string]*quic.Conn
}

func NewTunnelManager() *TunnelManager {
	return &TunnelManager{
		tunnels: make(map[string]*quic.Conn),
	}
}

// SendPacket 发送数据包，自动处理连接复用与重建
func (m *TunnelManager) SendPacket(
	ctx context.Context,
	remoteIP net.IP,
	pkt *tunnel_packet.Packet,
) error {
	if remoteIP == nil {
		return errors.New("remote ip is nil")
	}

	pkt.SerializeHead()
	data := pkt.Buf[:pkt.TotalBytes()]

	conn, err := m.GetOrCreateTunnel(ctx, remoteIP)
	if err != nil {
		return err
	}

	// 发送数据
	stream, err := conn.OpenUniStreamSync(ctx)
	if err != nil {
		// 发送失败，清理无效连接，下次自动重连
		m.CloseTunnel(remoteIP)
		return err
	}
	defer stream.Close()

	_, err = stream.Write(data)
	if err != nil {
		m.CloseTunnel(remoteIP)
	}
	return err
}

// GetOrCreateTunnel 获取连接，不存在则创建
func (m *TunnelManager) GetOrCreateTunnel(
	ctx context.Context,
	remoteIP net.IP,
) (*quic.Conn, error) {
	addr := net.JoinHostPort(remoteIP.String(), QUIC_PORT)

	// 1. 快速读取
	m.mu.RLock()
	conn, ok := m.tunnels[addr]
	m.mu.RUnlock()
	if ok {
		return conn, nil
	}

	// 2. 没找到，加锁创建
	m.mu.Lock()
	defer m.mu.Unlock()

	// 二次检查
	if conn, ok := m.tunnels[addr]; ok {
		return conn, nil
	}

	// 3. 新建 QUIC 连接
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

	// 4. 存入 map
	m.tunnels[addr] = conn
	log.Println("QUIC tunnel 已建立:", addr)
	return conn, nil
}

// CloseTunnel 关闭并删除连接
func (m *TunnelManager) CloseTunnel(remoteIP net.IP) {
	addr := net.JoinHostPort(remoteIP.String(), QUIC_PORT)

	m.mu.Lock()
	defer m.mu.Unlock()

	if conn, ok := m.tunnels[addr]; ok {
		_ = conn.CloseWithError(0, "connection failed")
		delete(m.tunnels, addr)
		log.Println("QUIC tunnel 已关闭:", addr)
	}
}

// GenerateTLSConfig 生成服务端证书
func GenerateTLSConfig() *tls.Config {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	template := &x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, _ := tls.X509KeyPair(pemCert, pemKey)
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"tunnel-quic"},
	}
}
