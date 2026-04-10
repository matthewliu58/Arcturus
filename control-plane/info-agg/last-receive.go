package info_agg

import (
	rece "control-plane/receive-info"
	"control-plane/util"
	"log/slog"
	"sync"
	"time"
)

type GlobalStats struct {
	mu           sync.RWMutex
	nodes        map[string]*rece.LastStats // key: nodeIP
	agg          map[string]*rece.LastStatsValue
	nodeLocation map[string][]string //key continent; val ips
}

func NewGlobalStats() *GlobalStats {
	return &GlobalStats{
		nodes:        make(map[string]*rece.LastStats),
		agg:          make(map[string]*rece.LastStatsValue),
		nodeLocation: make(map[string][]string),
	}
}

// AddOrUpdateNode 添加或更新节点（上报最新10分钟数据）
func (g *GlobalStats) AddOrUpdateNode(node *rece.LastStats) {
	if node.IP == "" {
		return
	}
	g.mu.Lock()
	g.nodes[node.IP] = node
	g.AddNodeLocation(node.IP, node.Continent)
	g.mu.Unlock()
}

// DelNode 删除下线节点
func (g *GlobalStats) DelNode(nodeIP string) {
	g.mu.Lock()
	delete(g.nodes, nodeIP)
	g.DelNodeLocation(nodeIP)
	g.mu.Unlock()
}

// AddNodeLocation 添加节点 IP 到地理位置索引
func (g *GlobalStats) AddNodeLocation(ip, continent string) {
	if g.nodeLocation[continent] == nil {
		g.nodeLocation[continent] = make([]string, 0)
	}
	// 避免重复
	for _, existingIP := range g.nodeLocation[continent] {
		if existingIP == ip {
			return
		}
	}
	g.nodeLocation[continent] = append(g.nodeLocation[continent], ip)
}

// DelNodeLocation 从地理位置索引删除节点 IP
func (g *GlobalStats) DelNodeLocation(ip string) {
	for continent, ips := range g.nodeLocation {
		for i, existingIP := range ips {
			if existingIP == ip {
				g.nodeLocation[continent] = append(ips[:i], ips[i+1:]...)
				return
			}
		}
	}
}

// GetNodeLocation 获取指定 continent 的所有节点 IP
// continent 为空返回所有
func (g *GlobalStats) GetNodeLocation(continent string) []string {
	if continent != "" {
		result := make([]string, len(g.nodeLocation[continent]))
		copy(result, g.nodeLocation[continent])
		return result
	}
	// 返回所有
	var result []string
	for _, ips := range g.nodeLocation {
		result = append(result, ips...)
	}
	return result
}

// GetAggMap 获取当前所有聚合好的数据（线程安全）
// 返回：map[聚合key]统计值
func (g *GlobalStats) GetAggMap() map[string]*rece.LastStatsValue {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// 复制一份返回，避免外部修改内部数据
	result := make(map[string]*rece.LastStatsValue, len(g.agg))
	for k, v := range g.agg {
		// 复制值对象，防止外部篡改
		valCopy := *v
		result[k] = &valCopy
	}

	return result
}

// GetAggValue 根据 key 获取单个聚合结果
func (g *GlobalStats) GetAggValue(key string) *rece.LastStatsValue {
	g.mu.RLock()
	defer g.mu.RUnlock()

	val, ok := g.agg[key]
	if !ok {
		return &rece.LastStatsValue{} // 不存在返回空值，不崩溃
	}

	// 返回副本，安全
	copy := *val
	return &copy
}

// 启动后台定时聚合（30秒刷新一次）
func (g *GlobalStats) StartAggregateWorker(logger *slog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			pre := util.GenerateRandomLetters(5)
			g.rebuildAggregate(pre, logger)
		}
	}()
}

// 重新计算所有地理维度聚合
func (g *GlobalStats) rebuildAggregate(pre string, logger *slog.Logger) {
	g.mu.RLock()
	nodeList := make([]*rece.LastStats, 0, len(g.nodes))
	for _, n := range g.nodes {
		nodeList = append(nodeList, n)
	}
	g.mu.RUnlock()

	newAgg := make(map[string]*rece.LastStatsValue)

	for _, node := range nodeList {
		for userKey, val := range node.DelayStats {

			g.merge(newAgg, userKey.City, node.IP, val)
			g.merge(newAgg, userKey.City, node.City, val)
			g.merge(newAgg, userKey.Country, node.Country, val)
			g.merge(newAgg, userKey.Continent, node.Continent, val)
		}
	}
	logger.Info("rebuildAggregate", slog.String("pre", pre), slog.Any("newAgg", newAgg))

	g.mu.Lock()
	g.agg = newAgg
	g.mu.Unlock()
}

// 合并统计：多个节点数据累加，计算平均
func (g *GlobalStats) merge(newAgg map[string]*rece.LastStatsValue, userKey, serverKey string, val *rece.LastStatsValue) {

	key := userKey + "-" + serverKey

	if newAgg[key] == nil {
		newAgg[key] = &rece.LastStatsValue{}
	}
	t := newAgg[key]

	t.Count += val.Count
	t.AvgRT += val.AvgRT
	if t.Count > 0 {
		t.SumRT = t.AvgRT / float64(t.Count)
	}
}
