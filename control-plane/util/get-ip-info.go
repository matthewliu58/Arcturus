package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

const (
	dataPlaneUrl = "http://127.0.0.1:7082"
)

// 完全匹配你的结构体
type IPInfoResult struct {
	IP          string `json:"ip"`
	Country     string `json:"country"`      // 国家
	CountryCode string `json:"country_code"` // 国家码
	Province    string `json:"province"`     // 省份
	City        string `json:"city"`         // 城市
	ISP         string `json:"isp"`          // 运营商
}

// GetIPInfo 根据IP查询信息（核心函数）
func GetIPInfo(ip string) (*IPInfoResult, error) {
	if ip == "" {
		return nil, errors.New("ip is empty")
	}

	// 构造请求 URL
	apiURL := fmt.Sprintf("%s/ip/info?ip=%s", dataPlaneUrl, url.QueryEscape(ip))

	// 发送请求
	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 解析返回
	var result IPInfoResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}
