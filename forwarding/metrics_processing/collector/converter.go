package collector

import (
	"forwarding/metrics_processing/protocol"
	"math"
)

// ConvertToProtoMetrics ， InfoData  protocol.Metrics
func ConvertToProtoMetrics(info InfoData) *protocol.Metrics {

	toMB := func(bytes uint64) uint64 {
		return (uint64(bytes) / (1024 * 1024) * 100) / 100 // MB
	}

	return &protocol.Metrics{
		Ip: info.IP,
		CpuInfo: &protocol.CPUInfo{
			Cores:     info.CPUInfo.Cores,
			ModelName: info.CPUInfo.ModelName,
			Mhz:       info.CPUInfo.Mhz,
			CacheSize: info.CPUInfo.CacheSize,

			Usage: math.Round(info.CPUInfo.Usage*100) / 100,
		},
		MemoryInfo: &protocol.MemoryInfo{

			Total:     toMB(info.MemoryInfo.Total),
			Available: toMB(info.MemoryInfo.Available),
			Used:      toMB(info.MemoryInfo.Used),

			UsedPercent: math.Round(info.MemoryInfo.UsedPercent*100) / 100,
		},
		DiskInfo: &protocol.DiskInfo{
			Device: info.DiskInfo.Device,

			Total: toMB(info.DiskInfo.Total),
			Free:  toMB(info.DiskInfo.Free),
			Used:  toMB(info.DiskInfo.Used),

			UsedPercent: math.Round(info.DiskInfo.UsedPercent*100) / 100,
		},
		NetworkInfo: &protocol.NetworkInfo{
			InterfaceName: info.NetworkInfo.InterfaceName,
			BytesSent:     info.NetworkInfo.BytesSent,
			BytesRecv:     info.NetworkInfo.BytesRecv,
			PacketsSent:   info.NetworkInfo.PacketsSent,
			PacketsRecv:   info.NetworkInfo.PacketsRecv,
		},
		HostInfo: &protocol.HostInfo{
			Hostname:        info.HostInfo.Hostname,
			Os:              info.HostInfo.OS,
			Platform:        info.HostInfo.Platform,
			PlatformVersion: info.HostInfo.PlatformVersion,
			Uptime:          info.HostInfo.Uptime,
		},
		LoadInfo: &protocol.LoadInfo{

			Load1:  math.Round(info.LoadInfo.Load1*100) / 100,
			Load5:  math.Round(info.LoadInfo.Load5*100) / 100,
			Load15: math.Round(info.LoadInfo.Load15*100) / 100,
		},
	}
}
