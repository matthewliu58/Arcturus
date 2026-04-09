package util

import (
	"encoding/binary"
	"net"
	"sync/atomic"
	"time"
)

// 全局原子自增序号
var seq uint64

// GenShortReqID 生成超短唯一ID：IP + 时间 + 自增序号（压缩版）
func GenShortReqID(clientIP string) string {
	// 1. 自增序号（原子安全）
	no := atomic.AddUint64(&seq, 1)

	// 2. 时间戳（秒级，转 36 进制，超短）
	timePart := time.Now().Unix()
	timeStr := toBase36(uint64(timePart))

	// 3. IP 转 2 字符（压缩）
	ip := net.ParseIP(clientIP).To4()
	ipPart := toBase36(uint64(binary.BigEndian.Uint16(ip[2:4])))

	// 4. 序号转 2 字符（永不重复）
	seqStr := toBase36(no)

	// 最终 ID：总长度 8~10 字符，超短！
	return ipPart + timeStr + seqStr
}

// toBase36 转 36 进制，缩短字符串
func toBase36(n uint64) string {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := 12
	for n > 0 && i > 0 {
		i--
		b[i] = chars[n%36]
		n /= 36
	}
	return string(b[i:])
}
