package aggregator

import (
	rece "control-plane/receive-info"
	"control-plane/sync/etcd_client"
	"control-plane/util"
	"encoding/json"
	"fmt"
	clientv3 "go.etcd.io/etcd/client/v3"
	"log/slog"
	"time"
)

const (
	expireTime = 1
)

type LinkCongestionInfo struct {
	TargetIP       string         `json:"target_ip"`
	Target         rece.ProbeTask `json:"target"`
	PacketLoss     float64        `json:"packet_loss"`
	AverageLatency float64        `json:"average_latency"`
}

type Telemetry struct {
	PublicIP        string                        `json:"public_ip"`
	Provider        string                        `json:"provider"`
	Continent       string                        `json:"continent"`
	Country         string                        `json:"country"`
	City            string                        `json:"city"`
	CpuPressure     float64                       `json:"cpu_pressure"`
	Cpu             rece.CPUInfo                  `json:"cpu"`
	LinksCongestion map[string]LinkCongestionInfo `json:"links_congestion"`
}

func CalcClusterWeightedAvg(fs *util.FileStorage, interval time.Duration,
	etcdClient *clientv3.Client, logPre string, logger *slog.Logger) {

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("CalcClusterWeightedAvg", slog.String("pre", logPre),
		slog.Duration("interval", interval), slog.String("storageDir", fs.StorageDir))

	type linksCongestion struct {
		TargetIP         string         `json:"target_ip"`
		PacketLosses     []float64      `json:"packet_losses"`
		AverageLatencies []float64      `json:"average_latencies"`
		ProbeTask        rece.ProbeTask `json:"probe_task"`
	}

	for {
		<-ticker.C

		pre := util.GenerateRandomLetters(5)

		allReports, err := fs.GetRecent(1, pre)
		if err != nil {
			logger.Warn("GetRecent report info failed", slog.String("pre", pre), slog.Any("err", err))
			continue
		}

		var cupPressureAvg float64
		var cpuAvg rece.CPUInfo
		totalLinksCong := make(map[string]linksCongestion)

		for _, report := range allReports {

			cpuAvg = report.CPU

			cupPressureAvg += report.CPU.Usage

			for _, v := range report.LinksCongestion {
				t, ok := totalLinksCong[v.TargetIP]
				if !ok {
					t = linksCongestion{
						TargetIP:  v.TargetIP,
						ProbeTask: v.Target,
					}
				}

				t.PacketLosses = append(t.PacketLosses, v.PacketLoss)
				t.AverageLatencies = append(t.AverageLatencies, v.AverageLatency)

				totalLinksCong[v.TargetIP] = t
			}
		}
		logger.Info("totalLinksCong info", slog.String("pre", pre), slog.Any("totalLinksCong", totalLinksCong))

		if cupPressureAvg <= 0 {
			cupPressureAvg = 0
		} else {
			cupPressureAvg = cupPressureAvg / float64(len(allReports))
		}

		linkMap := make(map[string]LinkCongestionInfo)
		for k, vs := range totalLinksCong {
			var avgLoss float64 = 0
			for _, v := range vs.PacketLosses {
				avgLoss += v
			}
			if avgLoss != 0 && len(vs.PacketLosses) > 0 {
				avgLoss = avgLoss / float64(len(vs.PacketLosses))
			}

			var avgLatency float64 = 0
			for _, v := range vs.AverageLatencies {
				avgLatency += v
			}
			if avgLatency != 0 && len(vs.AverageLatencies) > 0 {
				avgLatency = avgLatency / float64(len(vs.AverageLatencies))
			}

			linkMap[k] = LinkCongestionInfo{TargetIP: k, PacketLoss: avgLoss,
				Target: vs.ProbeTask, AverageLatency: avgLatency}
		}

		result := Telemetry{
			CpuPressure:     cupPressureAvg,
			LinksCongestion: linkMap,
			Cpu:             cpuAvg,
			PublicIP:        util.Config_.Node.IP.Public,
			Provider:        util.Config_.Node.Provider,
			Continent:       util.Config_.Node.Continent,
			Country:         util.Config_.Node.Country,
			City:            util.Config_.Node.City,
		}

		jsonData, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			logger.Warn("结构体JSON序列化失败，跳过本次发送",
				slog.String("pre", pre), slog.Any("err", err))
			continue
		}
		logger.Info("结构体JSON序列化成功", slog.String("pre", pre),
			slog.Any("data", string(jsonData)))

		ip := util.Config_.Node.IP.Public
		key := fmt.Sprintf("/routing/middle/%s", ip)
		//etcd_client.PutKey(etcdClient, key, string(jsonData), logPre, logger)
		_ = etcd_client.PutKeyWithLease(etcdClient, key, string(jsonData), int64(60*expireTime), pre, logger)

		logger.Info("定时计算完成", slog.String("pre", pre), slog.String("data", string(jsonData)))
	}
}
