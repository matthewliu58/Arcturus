package util

import (
	"encoding/binary"
	"net"
	"sync/atomic"
	"time"
)

var seq uint64

func GenShortReqID(clientIP string) uint32 {

	no := atomic.AddUint64(&seq, 1)

	now := time.Now().Unix()
	timeByte := byte(now & 0xFF)

	ip := net.ParseIP(clientIP).To4()
	ip16 := binary.BigEndian.Uint16(ip[2:4])
	
	id := (uint32(ip16) << 16) | (uint32(timeByte) << 8) | (uint32(no) & 0xFF)

	return id
}
