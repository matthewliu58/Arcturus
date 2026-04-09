package util

import (
	"net/http"
	"strings"
)

func GetPublicIP() (string, error) {
	resp, err := http.Get("https://icanhazip.com")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	buf := make([]byte, 1024)
	n, err := resp.Body.Read(buf)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf[:n])), nil
}
