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

type IPInfoResult struct {
	IP          string `json:"ip"`
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	Province    string `json:"province"`
	City        string `json:"city"`
	ISP         string `json:"isp"`
}

func GetIPInfo(ip string) (*IPInfoResult, error) {
	if ip == "" {
		return nil, errors.New("ip is empty")
	}

	apiURL := fmt.Sprintf("%s/ip/info?ip=%s", dataPlaneUrl, url.QueryEscape(ip))

	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result IPInfoResult
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}
