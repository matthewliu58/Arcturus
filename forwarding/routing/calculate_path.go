package routing

import (
	"forwarding/middle_mile_scheduling/common"
	"sync/atomic"
)

type WeightedRoundRobin struct {
	paths       []common.PathWithIP
	cumulative  []int
	totalWeight int
	current     uint32
}

func NewWeightedRoundRobin(paths []common.PathWithIP) *WeightedRoundRobin {

	cumulative := make([]int, len(paths))
	total := 0
	for i, p := range paths {
		total += p.Weight
		cumulative[i] = total
	}
	return &WeightedRoundRobin{
		paths:       paths,
		cumulative:  cumulative,
		totalWeight: total,
		current:     0,
	}
}

func (w *WeightedRoundRobin) Next() common.PathWithIP {
	if w.totalWeight == 0 || len(w.paths) == 0 {
		return common.PathWithIP{} //
	}

	n := atomic.AddUint32(&w.current, 1) - 1
	mod := int(n) % w.totalWeight

	for i, c := range w.cumulative {
		if mod < c {
			return w.paths[i]
		}
	}
	return common.PathWithIP{}
}
