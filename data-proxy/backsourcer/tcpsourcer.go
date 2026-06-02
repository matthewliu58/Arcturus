package backsourcer

import (
	"bufio"
	"net"
	"time"
)

type TCPProtocol struct {
	dialTimeout time.Duration
	ioTimeout   time.Duration
}

func NewTCPProtocol(dialTimeout, ioTimeout time.Duration) *TCPProtocol {
	return &TCPProtocol{
		dialTimeout: dialTimeout,
		ioTimeout:   ioTimeout,
	}
}

func (t *TCPProtocol) DoRequest(addr string, reqData []byte) ([]byte, error) {

	conn, err := net.DialTimeout("tcp", addr, t.dialTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(t.ioTimeout))

	_, err = conn.Write(reqData)
	if err != nil {
		return nil, err
	}

	// Read until newline delimiter instead of waiting for connection close
	reader := bufio.NewReader(conn)
	resp, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	return resp, nil
}
