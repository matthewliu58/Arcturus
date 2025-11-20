package metrics_storage

import (
	pb "scheduling/controller/metrics_processing/protocol"
	"sync"
)

type CacheManager struct {
	nodeMutex     sync.RWMutex
	taskMutex     sync.RWMutex
	domainIPMutex sync.RWMutex
	hashMutex     sync.RWMutex

	nodeListCache map[string]*pb.NodeList
	taskMapCache  map[string][]*pb.ProbeTask
	domainIPCache []*pb.DomainIPMapping
	hashCache     map[string]string
}

func NewCacheManager() *CacheManager {
	return &CacheManager{
		nodeListCache: make(map[string]*pb.NodeList),
		taskMapCache:  make(map[string][]*pb.ProbeTask),
		domainIPCache: make([]*pb.DomainIPMapping, 0),
		hashCache:     make(map[string]string),
	}
}

func (cm *CacheManager) SetNodeList(nodeList *pb.NodeList) {
	cm.nodeMutex.Lock()
	defer cm.nodeMutex.Unlock()
	cm.nodeListCache["global"] = nodeList
}

func (cm *CacheManager) GetNodeList() *pb.NodeList {
	cm.nodeMutex.RLock()
	defer cm.nodeMutex.RUnlock()
	return cm.nodeListCache["global"]
}

func (cm *CacheManager) SetTasks(ip string, tasks []*pb.ProbeTask) {
	cm.taskMutex.Lock()
	defer cm.taskMutex.Unlock()
	cm.taskMapCache[ip] = tasks
}

func (cm *CacheManager) GetTasks(ip string) []*pb.ProbeTask {
	cm.taskMutex.RLock()
	defer cm.taskMutex.RUnlock()
	return cm.taskMapCache[ip]
}

func (cm *CacheManager) SetAllTasks(taskMap map[string][]*pb.ProbeTask) {
	cm.taskMutex.Lock()
	defer cm.taskMutex.Unlock()
	cm.taskMapCache = taskMap
}

func (cm *CacheManager) GetAllTasks() map[string][]*pb.ProbeTask {
	cm.taskMutex.RLock()
	defer cm.taskMutex.RUnlock()

	result := make(map[string][]*pb.ProbeTask)
	for k, v := range cm.taskMapCache {
		result[k] = v
	}

	return result
}

func (cm *CacheManager) SetDomainIPMappings(mappings []*pb.DomainIPMapping) {
	cm.domainIPMutex.Lock()
	defer cm.domainIPMutex.Unlock()
	cm.domainIPCache = mappings
}

func (cm *CacheManager) GetDomainIPMappings() []*pb.DomainIPMapping {
	cm.domainIPMutex.RLock()
	defer cm.domainIPMutex.RUnlock()

	result := make([]*pb.DomainIPMapping, len(cm.domainIPCache))
	copy(result, cm.domainIPCache)

	return result
}

func (cm *CacheManager) SetHash(filePath, hash string) {
	cm.hashMutex.Lock()
	defer cm.hashMutex.Unlock()
	cm.hashCache[filePath] = hash
}

func (cm *CacheManager) GetHash(filePath string) string {
	cm.hashMutex.RLock()
	defer cm.hashMutex.RUnlock()
	return cm.hashCache[filePath]
}
