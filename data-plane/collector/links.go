package collector

import (
	"data-plane/probing"
	model "data-plane/report-info"
)

func BuildLinkCongestion() []model.LinkCongestion {
	results := probing.GetLatestResults()
	var links []model.LinkCongestion

	for target, r := range results {
		links = append(links, model.LinkCongestion{
			TargetIP:       target,
			Target:         r.Target,
			PacketLoss:     r.LossRate,
			AverageLatency: float64(r.AvgRTT.Milliseconds()),
		})
	}

	return links
}
