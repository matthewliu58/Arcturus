package util

import (
	"encoding/binary"
	"math/rand"
	"net"
	"time"
)

func GenerateRandomLetters(length int) string {
	rand.Seed(time.Now().UnixNano())
	letters := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var result string
	for i := 0; i < length; i++ {
		result += string(letters[rand.Intn(len(letters))])
	}
	return result
}

func HopIPToNet(ipStr string) net.IP {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil
	}
	return ip.To4()
}

func Uint32ToIP(ipUint32 uint32) net.IP {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, ipUint32)
	return net.IPv4(b[0], b[1], b[2], b[3]).To4()
}
