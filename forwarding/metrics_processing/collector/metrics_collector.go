package collector

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

type CPUInfo struct {
	Cores     int32
	ModelName string
	Mhz       float64
	CacheSize int32
	Usage     float64
}

type MemoryInfo struct {
	Total       uint64
	Available   uint64
	Used        uint64
	UsedPercent float64
}

type DiskInfo struct {
	Device      string
	Total       uint64
	Free        uint64
	Used        uint64
	UsedPercent float64
}

type NetworkInfo struct {
	InterfaceName string
	BytesSent     uint64
	BytesRecv     uint64
	PacketsSent   uint64
	PacketsRecv   uint64
}

type HostInfo struct {
	Hostname        string
	OS              string
	Platform        string
	PlatformVersion string
	Uptime          uint64
}

type LoadInfo struct {
	Load1  float64
	Load5  float64
	Load15 float64
}

type InfoData struct {
	IP          string
	CPUInfo     CPUInfo
	MemoryInfo  MemoryInfo
	DiskInfo    DiskInfo
	NetworkInfo NetworkInfo
	HostInfo    HostInfo
	LoadInfo    LoadInfo
}

// get public outbound ip addr
func GetIP() (string, error) {
	urls := []string{
		"http://icanhazip.com",
		"http://api.ipify.org",
		"http://ifconfig.me/ip",
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	var lastErr error
	for _, url := range urls {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = fmt.Errorf("failed to query %s: %w", url, err)
			continue
		}

		ipBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response from %s: %w", url, err)
			continue
		}

		ip := strings.TrimSpace(string(ipBytes))
		if ip != "" {
			return ip, nil
		}
	}

	return "", fmt.Errorf("all IP services failed: %w", lastErr)
}

func GetCPUInfo() (CPUInfo, error) {

	infos, err := cpu.Info()
	if err != nil {
		return CPUInfo{}, fmt.Errorf("failed to get CPU Info: %v", err)
	}

	usage, err := cpu.Percent(0, true)
	if err != nil {
		return CPUInfo{}, fmt.Errorf("failed to get CPU usage: %v", err)
	}

	var totalCores int32
	var modelName string
	var mhz float64
	var cacheSize int32

	if len(infos) > 0 {
		totalCores = infos[0].Cores
		modelName = infos[0].ModelName
		mhz = infos[0].Mhz
		cacheSize = infos[0].CacheSize
	}

	var totalUsage float64
	for _, u := range usage {
		totalUsage += u
	}
	averageUsage := totalUsage / float64(len(usage))

	return CPUInfo{
		Cores:     totalCores,
		ModelName: modelName,
		Mhz:       mhz,
		CacheSize: cacheSize,
		Usage:     averageUsage,
	}, nil
}

func GetMemoryInfo() (MemoryInfo, error) {

	v, err := mem.VirtualMemory()
	if err != nil {
		return MemoryInfo{}, fmt.Errorf("failed to get memory info: %v", err)
	}

	return MemoryInfo{
		Total:       v.Total,
		Available:   v.Available,
		Used:        v.Used,
		UsedPercent: v.UsedPercent,
	}, nil
}

func GetDiskInfo() (DiskInfo, error) {

	usage, err := disk.Usage("/")
	if err != nil {
		return DiskInfo{}, fmt.Errorf("failed to get disk usage: %v", err)
	}

	return DiskInfo{
		Device:      "Overall",
		Total:       usage.Total,
		Free:        usage.Free,
		Used:        usage.Used,
		UsedPercent: usage.UsedPercent,
	}, nil
}

func GetNetworkInfo() (NetworkInfo, error) {
	interfaces, err := net.IOCounters(true)
	if err != nil {
		return NetworkInfo{}, fmt.Errorf("failed to get network interfaces: %v", err)
	}

	for _, iface := range interfaces {
		if iface.Name != "lo" && iface.Name != "lo0" {
			return NetworkInfo{
				InterfaceName: iface.Name,
				BytesSent:     iface.BytesSent,
				BytesRecv:     iface.BytesRecv,
				PacketsSent:   iface.PacketsSent,
				PacketsRecv:   iface.PacketsRecv,
			}, nil
		}
	}

	return NetworkInfo{}, fmt.Errorf("no non-loopback interface found")
}

func GetHostInfo() (HostInfo, error) {

	info, err := host.Info()
	if err != nil {
		return HostInfo{}, fmt.Errorf("failed to get host info: %v", err)
	}

	// Get total TCP connections count
	tcpConnCount, err := GetTotalTCPConnections()
	if err != nil {
		return HostInfo{}, fmt.Errorf("failed to get TCP connections count: %v", err)
	}

	// Note: Hostname field actually stores the TCP connection count (as string) instead of hostname
	return HostInfo{
		Hostname:        strconv.Itoa(tcpConnCount), // TCP connection count
		OS:              info.OS,
		Platform:        info.Platform,
		PlatformVersion: info.PlatformVersion,
		Uptime:          info.Uptime,
	}, nil
}

func GetLoadInfo() (LoadInfo, error) {

	avg, err := load.Avg()
	if err != nil {
		return LoadInfo{}, fmt.Errorf("failed to get system load: %v", err)
	}

	return LoadInfo{
		Load1:  avg.Load1,
		Load5:  avg.Load5,
		Load15: avg.Load15,
	}, nil
}

func CollectSystemInfo() (InfoData, error) {
	ip, err := GetIP()
	if err != nil {
		return InfoData{}, err
	}

	cpuInfo, err := GetCPUInfo()
	if err != nil {
		return InfoData{}, err
	}

	memoryInfo, err := GetMemoryInfo()
	if err != nil {
		return InfoData{}, err
	}

	diskInfo, err := GetDiskInfo()
	if err != nil {
		return InfoData{}, err
	}

	networkInfo, err := GetNetworkInfo()
	if err != nil {
		return InfoData{}, err
	}

	hostInfo, err := GetHostInfo()
	if err != nil {
		return InfoData{}, err
	}

	loadInfo, err := GetLoadInfo()
	if err != nil {
		return InfoData{}, err
	}

	return InfoData{
		IP:          ip,
		CPUInfo:     cpuInfo,
		MemoryInfo:  memoryInfo,
		DiskInfo:    diskInfo,
		NetworkInfo: networkInfo,
		HostInfo:    hostInfo,
		LoadInfo:    loadInfo,
	}, nil
}

func GetTotalTCPConnections() (int, error) {
	// Get IPv4 TCP connection count
	ipv4Count, err := getTCPConnCountFromFile("/proc/net/tcp")
	if err != nil {
		return 0, fmt.Errorf("failed to get IPv4 TCP connection count: %w", err)
	}

	// Get IPv6 TCP connection count
	ipv6Count, err := getTCPConnCountFromFile("/proc/net/tcp6")
	if err != nil {
		return 0, fmt.Errorf("failed to get IPv6 TCP connection count: %w", err)
	}

	totalCount := ipv4Count + ipv6Count
	return totalCount, nil
}

// getTCPConnCountFromFile is an internal helper function that reads a single proc file and counts connections.
func getTCPConnCountFromFile(filePath string) (int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		// If the file doesn't exist (e.g., IPv6 is disabled on the system), this is not a fatal error, return 0.
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	// Simply count the number of lines
	for scanner.Scan() {
		count++
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("error reading file %s: %w", filePath, err)
	}

	// If the file is not empty, the first line is the header and should be subtracted.
	if count > 0 {
		return count - 1, nil
	}

	return 0, nil
}
