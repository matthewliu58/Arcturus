package collector

import (
	"data-plane/probing"
	model "data-plane/report-info"
)

func BuildLinkCongestion() []model.LinkCongestionInfo {
	results := probing.GetLatestResults()
	var links []model.LinkCongestionInfo

	for target, r := range results {
		links = append(links, model.LinkCongestionInfo{
			TargetIP:       target,
			Target:         r.Target,
			PacketLoss:     r.LossRate,
			AverageLatency: float64(r.AvgRTT.Milliseconds()),
		})
	}

	return links
}
