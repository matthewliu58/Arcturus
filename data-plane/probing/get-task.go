package probing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	TaskURL = "/api/v1/probe/tasks"
)

type ProbeTask struct {
	TargetType string `json:"TargetType"`
	Provider   string `json:"Provider"`
	IP         string `json:"IP"`
	Port       int    `json:"Port"`
	Region     string `json:"Region"`
	ID         string `json:"ID"`
}

func GetProbeTasks(controlHost, pre string) ([]ProbeTask, error) {

	url := controlHost + TaskURL

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("request api failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	var serverResp struct {
		Code int         `json:"code"`
		Msg  string      `json:"msg"`
		Data []ProbeTask `json:"data"`
	}

	if err = json.Unmarshal(body, &serverResp); err != nil {
		return nil, fmt.Errorf("parse json failed: %w", err)
	}

	if serverResp.Code != 200 {
		return nil, fmt.Errorf("api returned error: %d %s", serverResp.Code, serverResp.Msg)
	}

	return serverResp.Data, nil
}
