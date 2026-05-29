//go:build linux

package server

import (
	"net"

	"golang.org/x/sys/unix"
)

// getTCPRTT returns the kernel-level smoothed RTT (srtt) from TCP_INFO.
// This is the real network RTT maintained by the TCP stack via ACK timestamps,
// independent of application-layer queuing delays.
// Returns milliseconds, or -1 on error.
func getTCPRTT(conn net.Conn) float64 {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return -1
	}
	raw, err := tcpConn.SyscallConn()
	if err != nil {
		return -1
	}
	var info *unix.TCPInfo
	var innerErr error
	err = raw.Control(func(fd uintptr) {
		// GetsockoptTCPInfo returns a pointer to TCPInfo, not taking a pointer argument
		info, innerErr = unix.GetsockoptTCPInfo(int(fd), unix.SOL_TCP, unix.TCP_INFO)
	})
	if err != nil || innerErr != nil || info == nil {
		return -1
	}
	// info.Rtt is in microseconds
	return float64(info.Rtt) / 1000.0
}
