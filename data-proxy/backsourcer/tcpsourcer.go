package backsourcer

import (
	"io"
	"net"
	"time"
)

// TCPProtocol TCP 协议实现
type TCPProtocol struct {
	dialTimeout time.Duration
	ioTimeout   time.Duration
}

// NewTCPProtocol 创建 TCP 协议实例
func NewTCPProtocol(dialTimeout, ioTimeout time.Duration) *TCPProtocol {
	return &TCPProtocol{
		dialTimeout: dialTimeout,
		ioTimeout:   ioTimeout,
	}
}

// DoRequest 实现 OriginProtocol 接口
func (t *TCPProtocol) DoRequest(addr string, reqData []byte) ([]byte, error) {
	// 建立 TCP 连接
	conn, err := net.DialTimeout("tcp", addr, t.dialTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// 设置超时
	_ = conn.SetDeadline(time.Now().Add(t.ioTimeout))

	// 发送请求
	_, err = conn.Write(reqData)
	if err != nil {
		return nil, err
	}

	// 读取响应
	return io.ReadAll(conn)
}
