package info_agg

import (
	rece "control-plane/receive-info"
	"control-plane/sync/etcd_client"
	"control-plane/util"
	"encoding/json"
	"fmt"
	clientv3 "go.etcd.io/etcd/client/v3"
	"log/slog"
	"math"
	"time"
)

const (
	expireTime = 1 //小文件加速更看重performance 所以参考的周期更短
)

// 链路拥塞信息
type LinkCongestionInfo struct {
	TargetIP       string         `json:"target_ip"` // 目标节点 IP
	Target         rece.ProbeTask `json:"target"`
	PacketLoss     float64        `json:"packet_loss"`     // 丢包率，百分比
	AverageLatency float64        `json:"average_latency"` // 平均延迟（毫秒）
}

// 节点遥测数据
type Telemetry struct {
	PublicIP        string                        `json:"public_ip"` // 节点公网 IP
	Provider        string                        `json:"provider"`  // 云厂商
	Continent       string                        `json:"continent"` // 所属大洲
	CpuPressure     float64                       `json:"cpu_pressure"`
	Cpu             rece.CPUInfo                  `json:"cpu"`
	LinksCongestion map[string]LinkCongestionInfo `json:"links_congestion"` // 节点到其他节点的链路拥塞信息
}

func CalcClusterWeightedAvg(fs *util.FileStorage, interval time.Duration,
	etcdClient *clientv3.Client, logPre string, logger *slog.Logger) {

	// 1. 内嵌定时器，直接创建
	ticker := time.NewTicker(interval)
	defer ticker.Stop() // 程序退出时回收定时器资源

	// 2. 日志输出启动信息
	logger.Info("CalcClusterWeightedAvg", slog.String("pre", logPre),
		slog.Duration("interval", interval), slog.String("storageDir", fs.StorageDir))

	// 临时链路统计结构体，补充json tag适配JSON解析/序列化
	type linksCongestion struct {
		TargetIP         string         `json:"target_ip"`
		PacketLosses     []float64      `json:"packet_losses"`     // 若为单个丢包率则用packet_loss，数组用packet_losses
		AverageLatencies []float64      `json:"average_latencies"` // 单个延迟则用average_latency，数组用average_latencies
		ProbeTask        rece.ProbeTask `json:"probe_task"`
	}

	// 3. 无限循环，定时触发核心逻辑（复用GetAll()）
	for {
		// 监听定时器信号，到达间隔执行计算
		<-ticker.C

		pre := util.GenerateRandomLetters(5)

		// 获取所有VMReport数据
		allReports, err := fs.GetRecent(1, pre)
		if err != nil {
			logger.Warn("GetRecent report info failed", slog.String("pre", pre), slog.Any("err", err))
			continue
		}

		// 5. 初始化统计变量，执行核心计算
		var cupPressureAvg float64
		var cpuAvg rece.CPUInfo
		totalLinksCong := make(map[string]linksCongestion)

		// 6. 遍历GetAll()结果，累加统计值
		for _, report := range allReports {

			cpuAvg = report.CPU

			//cpu pressure
			cupPressureAvg += CalculateCPUPressureScore(report.CPU)

			for _, v := range report.LinksCongestion {
				// 1. 不存在则初始化
				t, ok := totalLinksCong[v.TargetIP]
				if !ok {
					t = linksCongestion{
						TargetIP:  v.TargetIP,
						ProbeTask: v.Target,
					}
				}

				// 2. 追加数据
				t.PacketLosses = append(t.PacketLosses, v.PacketLoss)
				t.AverageLatencies = append(t.AverageLatencies, v.AverageLatency)

				// 3. 写回 map
				totalLinksCong[v.TargetIP] = t
			}
		}
		logger.Info("totalLinksCong info", slog.String("pre", pre), slog.Any("totalLinksCong", totalLinksCong))

		if cupPressureAvg <= 0 {
			cupPressureAvg = 0
		} else {
			cupPressureAvg = cupPressureAvg / float64(len(allReports))
		}

		//简单求均值
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

		// 填充结果结构体
		result := Telemetry{
			CpuPressure:     cupPressureAvg,
			LinksCongestion: linkMap,
			Cpu:             cpuAvg,
			PublicIP:        util.Config_.Node.IP.Public,
			Provider:        util.Config_.Node.Provider,
			Continent:       util.Config_.Node.Continent,
		}

		// 4. 结构体序列化为JSON（Etcd存储二进制数据，JSON格式易解析）
		jsonData, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			logger.Warn("结构体JSON序列化失败，跳过本次发送",
				slog.String("pre", pre), slog.Any("err", err))
			continue
		}
		logger.Info("结构体JSON序列化成功", slog.String("pre", pre),
			slog.Any("data", string(jsonData)))

		// 5. 发送（写入）数据到Etcd（*clientv3.Client核心操作）
		//ip, _ := util.GetPublicIP()
		ip := util.Config_.Node.IP.Public
		key := fmt.Sprintf("/routing/middle/%s", ip)
		//etcd_client.PutKey(etcdClient, key, string(jsonData), logPre, logger)
		_ = etcd_client.PutKeyWithLease(etcdClient, key, string(jsonData), int64(60*expireTime), pre, logger)

		logger.Info("定时计算完成", slog.String("pre", pre), slog.String("data", string(jsonData)))
	}
}

func CalculateCPUPressureScore(cpu rece.CPUInfo) float64 {
	logical := float64(cpu.LogicalCore)
	if logical <= 0 {
		logical = 1
	}

	// 1. CPU 使用率贡献（0-70分）
	usageScore := math.Min(cpu.Usage, 100) * 0.7

	// 2. 系统负载贡献（0-30分）
	loadRatio := cpu.Load1Min / logical // 负载/核心数
	loadScore := math.Min(loadRatio*30, 30)

	// 总分 0~100
	score := usageScore + loadScore
	return math.Round(score*10) / 10 // 保留1位小数
}
