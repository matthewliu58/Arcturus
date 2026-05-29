//go:build !linux

package server

import "net"

func getTCPRTT(conn net.Conn) float64 {
	return -1
}
