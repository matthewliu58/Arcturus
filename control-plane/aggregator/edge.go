package aggregator

import (
	rece "control-plane/receive-info"
	"control-plane/util"
	"log/slog"
	"sync"
)

type GlobalStats struct {
	mu       sync.RWMutex
	nodeLast map[string]*rece.LastTelemetry
	edgeAgg  map[string]*rece.LastCongestion
}

func NewGlobalStats() *GlobalStats {
	return &GlobalStats{
		nodeLast: make(map[string]*rece.LastTelemetry),
		edgeAgg:  make(map[string]*rece.LastCongestion),
	}
}

func (g *GlobalStats) AddOrUpdateNode(node *rece.LastTelemetry) {
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

func (g *GlobalStats) GetAggMap() map[string]*rece.LastCongestion {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make(map[string]*rece.LastCongestion, len(g.edgeAgg))
	for k, v := range g.edgeAgg {
		valCopy := *v
		result[k] = &valCopy
	}

	return result
}

func (g *GlobalStats) GetAggValue(key string) *rece.LastCongestion {
	g.mu.RLock()
	defer g.mu.RUnlock()

	val, ok := g.edgeAgg[key]
	if !ok {
		return &rece.LastCongestion{}
	}

	copy_ := *val
	return &copy_
}

// RebuildAggregate triggers a single-pass rebuild of edge aggregate stats.
func (g *GlobalStats) RebuildAggregate(logger *slog.Logger) {
	pre := util.GenerateRandomLetters(5)
	g.rebuildAggregate(pre, logger)
}

func (g *GlobalStats) rebuildAggregate(pre string, logger *slog.Logger) {
	g.mu.RLock()
	nodeList := make([]*rece.LastTelemetry, 0, len(g.nodeLast))
	for _, n := range g.nodeLast {
		nodeList = append(nodeList, n)
	}
	g.mu.RUnlock()

	newAgg := make(map[string]*rece.LastCongestion)

	for _, node := range nodeList {
		for userKey, val := range node.LastsCongestion {
			// key = userCity -> nodeIP
			key := userKey.City + "-" + node.IP
			if newAgg[key] == nil {
				newAgg[key] = &rece.LastCongestion{}
			}
			s := newAgg[key]
			s.Count += val.Count
			s.SumRT += val.AvgRT * float64(val.Count)
		}
	}

	// Calculate average
	for _, s := range newAgg {
		if s.Count > 0 {
			s.AvgRT = s.SumRT / float64(s.Count)
		}
	}

	logger.Info("rebuildAggregate", slog.String("pre", pre), slog.Any("newAgg", newAgg))

	g.mu.Lock()
	g.edgeAgg = newAgg
	g.mu.Unlock()
}
