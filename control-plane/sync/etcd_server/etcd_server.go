package etcd_server

import (
	"log/slog"
	"net/url"
	"strings"

	"go.etcd.io/etcd/server/v3/embed"
)

func StartEmbeddedEtcd(serverList []string, serverIP, dataDir, name, pre string, logger *slog.Logger) (*embed.Etcd, error) {
	cfg := embed.NewConfig()
	cfg.Dir = dataDir
	cfg.Name = name

	// 当前节点 URL
	clientAddr, _ := url.Parse("http://" + serverIP + ":2379") //也可以0.0.0.0
	peerAddr, _ := url.Parse("http://" + serverIP + ":2380")

	cfg.ListenClientUrls = []url.URL{*clientAddr}
	cfg.AdvertiseClientUrls = []url.URL{*clientAddr}
	cfg.ListenPeerUrls = []url.URL{*peerAddr}
	cfg.AdvertisePeerUrls = []url.URL{*peerAddr}

	// 构造 InitialCluster 字符串
	var clusterEntries []string
	for _, ip := range serverList {
		nodeName := "etcd-" + strings.ReplaceAll(ip, ".", "-") // etcd-192-168-1-10
		clusterEntries = append(clusterEntries, nodeName+"=http://"+ip+":2380")
	}
	cfg.InitialCluster = strings.Join(clusterEntries, ",")
	cfg.ClusterState = embed.ClusterStateFlagNew

	// 启动 etcd
	e, err := embed.StartEtcd(cfg)
	if err != nil {
		return nil, err
	}

	<-e.Server.ReadyNotify()
	logger.Info("Embedded etcd started", slog.String("pre", pre),
		slog.String("name", name), slog.String("endpoint", serverIP+":2379"))
	return e, nil
}
