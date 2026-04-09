package collector

import (
	model "data-plane/report-info"
	"net/http"
	"strings"

	"github.com/shirou/gopsutil/v3/net"
	net1 "net"
)

// collectNetwork 采集网络信息（含外网IP、端口数、流量）
func collectNetwork() (model.NetworkInfo, error) {
	// 1. 获取外网IP
	publicIP, err := getPublicIP()
	if err != nil {
		publicIP = "no-public-ip"
	}

	// 2. 获取内网IP（取第一个非回环地址）
	privateIP := getPrivateIP()

	// 3. 获取端口占用数
	ports, err := net.Connections("all")
	if err != nil {
		return model.NetworkInfo{}, err
	}
	portCount := len(ports)

	// 4. 获取网卡流量
	ioStat, err := net.IOCounters(true)
	if err != nil {
		return model.NetworkInfo{}, err
	}
	trafficIn := uint64(0)
	trafficOut := uint64(0)
	for _, stat := range ioStat {
		trafficIn += stat.BytesRecv
		trafficOut += stat.BytesSent
	}

	return model.NetworkInfo{
		PublicIP:   publicIP,
		PrivateIP:  privateIP,
		PortCount:  portCount,
		TrafficIn:  trafficIn,
		TrafficOut: trafficOut,
	}, nil
}

// getPublicIP 获取外网IP
func getPublicIP() (string, error) {
	resp, err := http.Get("https://icanhazip.com")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	buf := make([]byte, 1024)
	n, err := resp.Body.Read(buf)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf[:n])), nil
}

// getPrivateIP 获取内网IPv4地址（跳过回环、仅取第一个有效IPv4）
// 依赖：标准库net + strings，无第三方依赖
func getPrivateIP() string {
	// 1. 获取所有网络接口（标准库net.Interface列表）
	interfaces, err := net1.Interfaces()
	if err != nil {
		return ""
	}

	// 2. 遍历每个网络接口
	for _, iface := range interfaces {
		// 跳过回环接口（FlagLoopback是标准库net的常量）
		if iface.Flags&net1.FlagLoopback != 0 {
			continue
		}
		// 跳过未启动的接口（必须有UP标识）
		if iface.Flags&net1.FlagUp == 0 {
			continue
		}

		// 3. 获取当前接口的所有地址（标准库net.Addr列表）
		addrList, err := iface.Addrs()
		if err != nil {
			continue
		}

		// 4. 遍历地址，提取IPv4
		for _, addr := range addrList {
			// 类型断言：仅处理IPNet类型（排除unix socket等）
			ipNet, ok := addr.(*net1.IPNet)
			if !ok || ipNet.IP.IsLoopback() {
				continue
			}

			// 提取纯IPv4地址（跳过IPv6）
			ipv4 := ipNet.IP.To4()
			if ipv4 != nil {
				return ipv4.String() // 返回第一个有效IPv4
			}
		}
	}

	// 无有效内网IP
	return ""
}
