package routing

import (
	"control-plane/storage"
	"fmt"
	"log/slog"
	"math"
	"strings"
)

type Path struct {
	path []string
	cost float64
}

type PathInfo struct {
	Hops string `json:"hops"`
	Rate int64  `json:"rate"`
	//Weight int64  `json:"weight"`
}

type RoutingInfo struct {
	Routing []PathInfo `json:"routing"`
}

type EndPoint struct {
	IP       string `json:"ip"`
	Provider string `json:"provider"`
	Region   string `json:"region"`
	ID       string `json:"id"`
}

type EndPoints struct {
	Source EndPoint `json:"source"`
	Dest   EndPoint `json:"dest"`
}

// 输入是client区域和cloud storage 区域
func (g *GraphManager) Routing(endPoints EndPoints, pre string, logger *slog.Logger) RoutingInfo {

	logger.Info("Routing", slog.String("pre", pre), slog.Any("endPoints", endPoints))

	// 获取所有节点
	allNodes := g.GetNodes()

	// 根据大洲过滤 start 和 end 节点 && 展现寻找最优路径
	var startNodes []*storage.NetworkTelemetry

	// todo 是否要偏向同运营商？
	for _, node := range allNodes {
		//proxy部署client逻辑, 优先走自己
		if node.PublicIP == endPoints.Source.IP {
			startNodes = append(startNodes, node)
			break
		}
		if node.Continent == endPoints.Source.Region {
			startNodes = append(startNodes, node)
		}
	}

	//todo 即使client所在区域没有覆盖也可以提供routing
	if len(startNodes) == 0 {
		logger.Warn("No nodes found for start continent", slog.String("pre", pre))
		return RoutingInfo{}
	}

	serverFull := fmt.Sprintf("%s_%s_%s", endPoints.Dest.Provider, endPoints.Dest.Region, endPoints.Dest.ID)

	// 遍历 start × end 节点组合，寻找最短路径
	var bestPath []string
	var tempPaths []Path
	minCost := math.Inf(1)
	for _, sNode := range startNodes {
		path, cost := g.Dijkstra(InNode(sNode.PublicIP), serverFull)
		if path == nil {
			continue
		}
		tempPaths = append(tempPaths, Path{path, cost})
		if len(path) > 0 && cost < minCost {
			minCost = cost
			bestPath = path
		}
	}
	logger.Info("All candidate paths", slog.String("pre", pre),
		slog.String("paths", fmt.Sprintf("%+v", tempPaths)))

	if len(bestPath) == 0 {
		logger.Warn("No cloud node found", slog.String("pre", pre), slog.String("serverFull", serverFull))
		return RoutingInfo{}
	}

	// 输出结果
	if len(bestPath) == 0 {
		logger.Warn("No path found between continents", slog.String("pre", pre),
			slog.String("startContinent", endPoints.Source.Region),
			slog.String("endContinent", serverFull))
	} else {
		logger.Info("Shortest path found", slog.String("pre", pre),
			slog.String("startContinent", endPoints.Source.Region),
			slog.String("endContinent", serverFull),
			slog.Any("path", bestPath), slog.Any("totalRisk", minCost))
	}

	var hops []string
	hopMap := make(map[string]string)
	for _, h := range bestPath {
		tempIP := strings.Split(h, "-")[0]
		if _, ok := hopMap[tempIP]; !ok {
			hops = append(hops, tempIP)
			hopMap[tempIP] = tempIP
		}
	}
	var hops_ []string
	for i := 0; i < len(hops)-1; i++ { //去掉最后一个 后面替换成真实的ip:port
		hops_ = append(hops_, hops[i]+":8090") //gateway port
	}
	merged := strings.Join(hops_, ",")
	merged += "," + endPoints.Dest.IP

	//计算速率
	var paths []PathInfo
	rate := ComputeAdmissionRate(Task{WeightU: 1, MinRate: 10, MaxRate: 20}, minCost, 1.0, 100, pre, g.logger)
	paths = append(paths, PathInfo{Hops: merged, Rate: int64(rate)})
	rout := RoutingInfo{Routing: paths}

	logger.Info("routing result", slog.String("pre", pre), slog.Any("rout", rout))
	return rout
}
