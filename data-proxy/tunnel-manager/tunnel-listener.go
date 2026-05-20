package tunnel_manager

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"time"

	packet "data-proxy/tunnel-packet"

	"github.com/quic-go/quic-go"
)

const streamConcurrentLimit = 64 * 2

var streamSem = make(chan struct{}, streamConcurrentLimit)

func ListenAndServeQUIC(handler func(remoteAddr string, data []byte, l *slog.Logger), pre string, l *slog.Logger) error {
	tlsConfig := GenerateTLSConfig()
	addr := net.JoinHostPort("0.0.0.0", QUIC_PORT)

	quicConf := &quic.Config{
		MaxIncomingUniStreams: 1000,
		MaxIncomingStreams:    1000,
		KeepAlivePeriod:       30 * time.Second,
	}

	ln, err := quic.ListenAddr(addr, tlsConfig, quicConf)
	if err != nil {
		return err
	}
	l.Info("QUIC listener started", slog.String("pre", pre), slog.String("addr", addr))

	for {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			l.Warn("Failed to accept QUIC connection", slog.String("pre", pre), slog.Any("err", err))
			continue
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
			l.Error("Accept Uni Stream failed", slog.String("remote", remote), slog.Any("err", err))
			return
		}

		streamSem <- struct{}{}
		localStream := stream
		go func() {
			defer func() {
				<-streamSem
				localStream.CancelRead(0)
			}()

			headerBuf := make([]byte, packet.HeaderSize)
			_, err := io.ReadFull(localStream, headerBuf)
			if err != nil {
				l.Error("Read header buf failed", slog.String("remote", remote), slog.Any("err", err))
				return
			}

			payloadLen := binary.BigEndian.Uint16(headerBuf[17:19])
			totalLen := packet.HeaderSize + int(payloadLen)

			buf := make([]byte, totalLen)
			copy(buf, headerBuf)

			_, err = io.ReadFull(localStream, buf[packet.HeaderSize:])
			if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
				l.Error("Read content failed", slog.String("remote", remote), slog.Any("err", err))
				return
			}

			handler(remote, buf, l)
		}()
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
