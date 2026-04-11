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
	nodeLasts    map[string]*rece.LastStats // key: nodeIP
	edgeAggs     map[string]*rece.LastStatsVal
	nodeLocation map[string][]string //key continent; val ips
}

func NewGlobalStats() *GlobalStats {
	return &GlobalStats{
		nodeLasts:    make(map[string]*rece.LastStats),
		edgeAggs:     make(map[string]*rece.LastStatsVal),
		nodeLocation: make(map[string][]string),
	}
}

// AddOrUpdateNode 添加或更新节点（上报最新10分钟数据）
func (g *GlobalStats) AddOrUpdateNode(node *rece.LastStats) {
	if node.IP == "" {
		return
	}
	g.mu.Lock()
	g.nodeLasts[node.IP] = node
	g.AddNodeLocation(node.IP, node.Continent)
	g.mu.Unlock()
}

// DelNode 删除下线节点
func (g *GlobalStats) DelNode(nodeIP string) {
	g.mu.Lock()
	delete(g.nodeLasts, nodeIP)
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

func (g *GlobalStats) GetNodeLocation() map[string][]string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodeLocation := make(map[string][]string)
	for c, ips := range g.nodeLocation {
		nodeLocation[c] = ips
	}
	return nodeLocation
}

// GetAggMap 获取当前所有聚合好的数据（线程安全）
// 返回：map[聚合key]统计值
func (g *GlobalStats) GetAggMap() map[string]*rece.LastStatsVal {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// 复制一份返回，避免外部修改内部数据
	result := make(map[string]*rece.LastStatsVal, len(g.edgeAggs))
	for k, v := range g.edgeAggs {
		// 复制值对象，防止外部篡改
		valCopy := *v
		result[k] = &valCopy
	}

	return result
}

// GetAggValue 根据 key 获取单个聚合结果
func (g *GlobalStats) GetAggValue(key string) *rece.LastStatsVal {
	g.mu.RLock()
	defer g.mu.RUnlock()

	val, ok := g.edgeAggs[key]
	if !ok {
		return &rece.LastStatsVal{} // 不存在返回空值，不崩溃
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
	nodeList := make([]*rece.LastStats, 0, len(g.nodeLasts))
	for _, n := range g.nodeLasts {
		nodeList = append(nodeList, n)
	}
	g.mu.RUnlock()

	newAggs := make(map[string]*rece.LastStatsVal)

	for _, node := range nodeList {
		for userKey, val := range node.DelayStats {

			g.merge(newAggs, userKey.City, node.IP, val)
			g.merge(newAggs, userKey.City, node.City, val)
			g.merge(newAggs, userKey.Country, node.Country, val)
			g.merge(newAggs, userKey.Continent, node.Continent, val)
		}
	}
	logger.Info("rebuildAggregate", slog.String("pre", pre), slog.Any("newAggs", newAggs))

	g.mu.Lock()
	g.edgeAggs = newAggs
	g.mu.Unlock()
}

// 合并统计：多个节点数据累加，计算平均
func (g *GlobalStats) merge(newAgg map[string]*rece.LastStatsVal, userKey, serverKey string, val *rece.LastStatsVal) {

	key := userKey + "-" + serverKey

	if newAgg[key] == nil {
		newAgg[key] = &rece.LastStatsVal{}
	}
	t := newAgg[key]

	t.Count += val.Count
	t.AvgRT += val.AvgRT
	if t.Count > 0 {
		t.SumRT = t.AvgRT / float64(t.Count)
	}
}
