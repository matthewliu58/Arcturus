package common

import (
	"fmt"
	msc "forwarding/middle_mile_scheduling/common"
	"sync"

	log "github.com/sirupsen/logrus"
)

type TopologyManager struct {
	Topology    *TopologyGraph
	Network     *msc.Network
	IPToIndex   map[string]int
	IndexToIP   map[int]string
	mutex       sync.RWMutex
	Initialized bool
}

type TopologyGraph struct {
	Links map[string]map[string]float32
	mutex sync.RWMutex
}

var (
	instance *TopologyManager
	once     sync.Once
)

func GetInstance() *TopologyManager {
	once.Do(func() {
		instance = &TopologyManager{
			Topology:    nil,
			Network:     nil,
			IPToIndex:   make(map[string]int),
			IndexToIP:   make(map[int]string),
			Initialized: false,
		}
	})
	return instance
}

func NewTopologyGraph() *TopologyGraph {
	return &TopologyGraph{
		Links: make(map[string]map[string]float32),
	}
}

func (tm *TopologyManager) SetTopology(topology *TopologyGraph) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	tm.Topology = topology

	allNodes := topology.GetAllNodes()
	tm.IPToIndex = make(map[string]int)
	tm.IndexToIP = make(map[int]string)

	for i, ip := range allNodes {
		tm.IPToIndex[ip] = i
		tm.IndexToIP[i] = ip
	}

	tm.Network = &msc.Network{
		Nodes: make([]msc.Node, len(allNodes)),
		Links: make([][]int, len(allNodes)),
	}

	for i := range tm.Network.Links {
		tm.Network.Links[i] = make([]int, len(allNodes))
		for j := range tm.Network.Links[i] {
			tm.Network.Links[i][j] = -1
		}
	}

	for sourceIP, targetLinks := range tm.Topology.Links {
		sourceIdx := tm.IPToIndex[sourceIP]
		for targetIP, weight := range targetLinks {
			targetIdx := tm.IPToIndex[targetIP]

			tm.Network.Links[sourceIdx][targetIdx] = int(weight)
		}
	}

	tm.Initialized = true
	log.Infof("SetTopology, node num: %d , link num: %d ", len(allNodes), topology.LinkCount())
}

func (tm *TopologyManager) IsInitialized() bool {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	return tm.Initialized
}

// GetNetwork returns a copy of the current network topology with IP mappings
func (tm *TopologyManager) GetNetwork() (msc.Network, map[string]int, map[int]string, error) {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	if !tm.Initialized {
		return msc.Network{}, nil, nil, fmt.Errorf("topology not yet initialized")
	}

	networkCopy := msc.Network{
		Nodes: make([]msc.Node, len(tm.Network.Nodes)),
		Links: make([][]int, len(tm.Network.Links)),
	}

	for i := range tm.Network.Links {
		networkCopy.Links[i] = make([]int, len(tm.Network.Links[i]))
		copy(networkCopy.Links[i], tm.Network.Links[i])
	}

	ipToIndexCopy := make(map[string]int)
	indexToIPCopy := make(map[int]string)

	for ip, idx := range tm.IPToIndex {
		ipToIndexCopy[ip] = idx
	}

	for idx, ip := range tm.IndexToIP {
		indexToIPCopy[idx] = ip
	}

	return networkCopy, ipToIndexCopy, indexToIPCopy, nil
}

func (t *TopologyGraph) AddLink(sourceIP, targetIP string, weight float32) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if _, exists := t.Links[sourceIP]; !exists {
		t.Links[sourceIP] = make(map[string]float32)
	}

	t.Links[sourceIP][targetIP] = weight
}

func (t *TopologyGraph) GetLinkWeight(sourceIP, targetIP string) (float32, bool) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	if targetLinks, exists := t.Links[sourceIP]; exists {
		if weight, exists := targetLinks[targetIP]; exists {
			return weight, true
		}
	}

	return 0, false
}

func (t *TopologyGraph) GetOutgoingLinks(sourceIP string) map[string]float32 {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	result := make(map[string]float32)
	if targetLinks, exists := t.Links[sourceIP]; exists {
		for targetIP, weight := range targetLinks {
			result[targetIP] = weight
		}
	}

	return result
}

func (t *TopologyGraph) NodeCount() int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	nodeSet := make(map[string]bool)

	for sourceIP := range t.Links {
		nodeSet[sourceIP] = true
	}

	for _, targetLinks := range t.Links {
		for targetIP := range targetLinks {
			nodeSet[targetIP] = true
		}
	}

	return len(nodeSet)
}

func (t *TopologyGraph) LinkCount() int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	count := 0
	for _, targetLinks := range t.Links {
		count += len(targetLinks)
	}

	return count
}

func (t *TopologyGraph) GetAllNodes() []string {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	nodeSet := make(map[string]bool)

	for sourceIP := range t.Links {
		nodeSet[sourceIP] = true
	}

	for _, targetLinks := range t.Links {
		for targetIP := range targetLinks {
			nodeSet[targetIP] = true
		}
	}

	nodes := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		nodes = append(nodes, node)
	}

	return nodes
}
