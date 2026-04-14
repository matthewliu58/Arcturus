package middle_collector

import (
	model "data-plane/report-info"
	"data-plane/util"
	"github.com/shirou/gopsutil/v3/net"
)

func collectNetwork() (model.NetworkInfo, error) {

	publicIP := util.Config_.Node.IP.Public
	privateIP := util.Config_.Node.IP.Private

	ports, err := net.Connections("all")
	if err != nil {
		return model.NetworkInfo{}, err
	}
	portCount := len(ports)
	
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
