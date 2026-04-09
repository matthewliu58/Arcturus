package collector

import (
	"data-plane/probing"
	model "data-plane/report-info"
)

// BuildLinkCongestion 从最新探测结果生成链路拥塞信息
func BuildLinkCongestion() []model.LinkCongestionInfo {
	results := probing.GetLatestResults()
	var links []model.LinkCongestionInfo

	for target, r := range results {
		links = append(links, model.LinkCongestionInfo{
			TargetIP:       target,
			Target:         r.Target,
			PacketLoss:     r.LossRate,
			AverageLatency: float64(r.AvgRTT.Milliseconds()), // 毫秒
		})
	}

	return links
}
