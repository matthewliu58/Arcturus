package probing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	ProbingTaskURL = "/api/v1/probe/tasks"
)

type ProbeTask struct {
	TargetType string `json:"TargetType"`
	Provider   string `json:"Provider"`
	IP         string `json:"IP"`
	Port       int    `json:"Port"`
	Region     string `json:"Region"`
	ID         string `json:"ID"`
}

func GetProbeTasks(pre, controlHost string) ([]ProbeTask, error) {

	url := controlHost + ProbingTaskURL

	// 1. 发起 HTTP GET 请求
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("请求接口失败: %w", err)
	}
	defer resp.Body.Close()

	// 2. 读取响应 body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 3. 定义服务端返回结构
	var serverResp struct {
		Code int         `json:"code"`
		Msg  string      `json:"msg"`
		Data []ProbeTask `json:"data"`
	}

	// 4. 解析 JSON
	if err := json.Unmarshal(body, &serverResp); err != nil {
		return nil, fmt.Errorf("解析JSON失败: %w", err)
	}

	// 5. 检查返回码
	if serverResp.Code != 200 {
		return nil, fmt.Errorf("接口返回错误: %d %s", serverResp.Code, serverResp.Msg)
	}

	// 6. 返回节点列表
	return serverResp.Data, nil
}
