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

	packet "data-proxy/tunnel-packet"

	"github.com/quic-go/quic-go"
)

func ListenAndServeQUIC(handler func(remoteAddr string, data []byte, l *slog.Logger), pre string, l *slog.Logger) error {
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

		buf := make([]byte, totalLen)
		copy(buf, headerBuf)

		_, err = io.ReadFull(stream, buf[packet.HeaderSize:])
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			l.Error("ReadFull failed", slog.String("remote", remote), slog.Any("err", err))
			continue
		}

		handler(remote, buf, l)
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
