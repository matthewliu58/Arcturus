package tunnel_manager

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"data-proxy/config"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"

	packet "data-proxy/tunnel-packet"

	"github.com/quic-go/quic-go"
)

const defaultPoolSize = 1024
const defaultPoolCount = 256

// bufferedPool is a bounded pool of byte buffers
type bufferedPool struct {
	pool    chan []byte
	bufSize int
}

func newBufferedPool(count, bufSize int) *bufferedPool {
	p := &bufferedPool{
		pool:    make(chan []byte, count),
		bufSize: bufSize,
	}
	for i := 0; i < count; i++ {
		p.pool <- make([]byte, bufSize)
	}
	return p
}

func (p *bufferedPool) Get() []byte {
	select {
	case buf := <-p.pool:
		return buf[:p.bufSize]
	default:
		return make([]byte, p.bufSize)
	}
}

func (p *bufferedPool) Put(buf []byte) {
	if cap(buf) < p.bufSize {
		return
	}
	select {
	case p.pool <- buf[:p.bufSize]:
	default:
		// pool full, discard
	}
}

// global buffer pool instance
var bufferPool *bufferedPool

func initBufferPool() {
	size := defaultPoolSize
	count := defaultPoolCount
	if config.Config_ != nil && config.Config_.Aggregator.BufferSize > 0 {
		size = config.Config_.Aggregator.BufferSize
	}
	bufferPool = newBufferedPool(count, size)
}

func ListenAndServeQUIC(handler func(remoteAddr string, data []byte, l *slog.Logger), pre string, l *slog.Logger) error {
	initBufferPool()

	tlsConfig := GenerateTLSConfig()
	addr := net.JoinHostPort("0.0.0.0", QUIC_PORT)

	ln, err := quic.ListenAddr(addr, tlsConfig, &quic.Config{})
	if err != nil {
		return err
	}
	l.Info("QUIC listener started", slog.String("pre", pre), slog.String("addr", addr))

	for {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			l.Error("Failed to accept QUIC connection", slog.String("pre", pre), slog.Any("err", err))
			return err
		}
		go handleConn(conn, handler, pre, l)
	}
}

func handleConn(conn *quic.Conn, handler func(remoteAddr string, data []byte, l *slog.Logger), pre string, l *slog.Logger) {
	defer conn.CloseWithError(0, "exit")
	remote := conn.RemoteAddr().String()
	l.Info("Connected", slog.String("remote", remote))

	for {
		stream, err := conn.AcceptUniStream(context.Background())
		if err != nil {
			l.Error("AcceptUniStream failed", slog.String("remote", remote), slog.Any("err", err))
			return
		}

		headerBuf := make([]byte, packet.HeaderSize)
		_, err = io.ReadFull(stream, headerBuf)
		if err != nil {
			l.Error("ReadFull failed", slog.String("remote", remote), slog.Any("err", err))
			continue
		}

		payloadLen := binary.BigEndian.Uint16(headerBuf[17:19])
		totalLen := packet.HeaderSize + int(payloadLen)

		buf := bufferPool.Get()
		usePool := true

		if cap(buf) < totalLen {
			buf = make([]byte, totalLen)
			usePool = false
		} else {
			buf = buf[:totalLen]
		}

		copy(buf, headerBuf)

		_, err = io.ReadFull(stream, buf[packet.HeaderSize:])
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			l.Error("ReadFull failed", slog.String("remote", remote), slog.Any("err", err))
			if usePool {
				bufferPool.Put(buf)
			}
			continue
		}

		go func(handleBuf []byte, fromPool bool) {
			if fromPool {
				defer bufferPool.Put(handleBuf)
			}
			handler(remote, handleBuf, l)
		}(buf, usePool)
	}
}

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
