package aggregator

import (
	rece "control-plane/receive-info"
	"control-plane/util"
	"log/slog"
	"sync"
	"time"
)

type GlobalStats struct {
	mu       sync.RWMutex
	nodeLast map[string]*rece.LastStats
	edgeAgg  map[string]*rece.LastStatsVal
	//nodeLocation map[string][]string
}

func NewGlobalStats() *GlobalStats {
	return &GlobalStats{
		nodeLast: make(map[string]*rece.LastStats),
		edgeAgg:  make(map[string]*rece.LastStatsVal),
		//nodeLocation: make(map[string][]string),
	}
}

func (g *GlobalStats) AddOrUpdateNode(node *rece.LastStats) {
	if node.IP == "" {
		return
	}
	g.mu.Lock()
	g.nodeLast[node.IP] = node
	g.mu.Unlock()
}

func (g *GlobalStats) DelNode(nodeIP string) {
	g.mu.Lock()
	delete(g.nodeLast, nodeIP)
	g.mu.Unlock()
}

func (g *GlobalStats) GetAggMap() map[string]*rece.LastStatsVal {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make(map[string]*rece.LastStatsVal, len(g.edgeAgg))
	for k, v := range g.edgeAgg {
		valCopy := *v
		result[k] = &valCopy
	}

	return result
}

func (g *GlobalStats) GetAggValue(key string) *rece.LastStatsVal {
	g.mu.RLock()
	defer g.mu.RUnlock()

	val, ok := g.edgeAgg[key]
	if !ok {
		return &rece.LastStatsVal{}
	}

	copy_ := *val
	return &copy_
}

func (g *GlobalStats) StartAggregateWorker(logger *slog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			pre := util.GenerateRandomLetters(5)
			g.rebuildAggregate(pre, logger)
		}
	}()
}

func (g *GlobalStats) rebuildAggregate(pre string, logger *slog.Logger) {
	g.mu.RLock()
	nodeList := make([]*rece.LastStats, 0, len(g.nodeLast))
	for _, n := range g.nodeLast {
		nodeList = append(nodeList, n)
	}
	g.mu.RUnlock()

	newAgg := make(map[string]*rece.LastStatsVal)

	for _, node := range nodeList {
		for userKey, val := range node.DelayStats {

			if userKey.Continent == node.Continent {
				g.merge(newAgg, userKey.City, node.IP, val)
				g.merge(newAgg, userKey.City, node.City, val)
				g.merge(newAgg, userKey.Country, node.Country, val)
				g.merge(newAgg, userKey.Continent, node.Continent, val)
			} else {
				g.merge(newAgg, userKey.Continent, "general", val)
			}

		}
	}
	logger.Info("rebuildAggregate", slog.String("pre", pre), slog.Any("newAgg", newAgg))

	g.mu.Lock()
	g.edgeAgg = newAgg
	g.mu.Unlock()
}

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
