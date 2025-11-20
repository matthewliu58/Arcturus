package goroutine_pool

import (
	"github.com/panjf2000/ants/v2"
	log "github.com/sirupsen/logrus"
	"sync"
)

const (
	ConfigPushPool  = "config_push"
	RegionTasksPool = "region_tasks"
	IPTasksPool     = "ip_tasks"
	EtcdSyncPool    = "etcd_sync_pool"
)

var (
	pools     = make(map[string]*ants.PoolWithFunc)
	poolsLock sync.RWMutex
)

func InitPool(poolType string, poolSize int, taskFunc func(interface{})) {
	poolsLock.Lock()
	defer poolsLock.Unlock()

	if p, exists := pools[poolType]; exists {
		p.Release()
	}

	pool, err := ants.NewPoolWithFunc(poolSize, taskFunc)
	if err != nil {
		log.Errorf("NewPoolWithFunc failed, poolType=%s : err=%v", poolType, err)
		return
	}

	pools[poolType] = pool
}

func GetPool(poolType string) *ants.PoolWithFunc {
	poolsLock.RLock()
	defer poolsLock.RUnlock()

	return pools[poolType]
}

func ReleasePool(poolType string) {
	poolsLock.Lock()
	defer poolsLock.Unlock()

	if p, exists := pools[poolType]; exists {
		p.Release()
		delete(pools, poolType)
	}
}

func ReleaseAllPools() {
	poolsLock.Lock()
	defer poolsLock.Unlock()

	for k, p := range pools {
		p.Release()
		delete(pools, k)
	}
}

//----------- API -----------

var (
	legacyPool *ants.PoolWithFunc
	once       sync.Once
)

func InitPoolLegacy(poolSize int, taskFunc func(interface{})) {
	once.Do(func() {
		var err error
		legacyPool, err = ants.NewPoolWithFunc(poolSize, taskFunc)
		if err != nil {
			log.Fatalf("Failed to create goroutine_pool, err=%v", err)
		}
	})
}

func GetPoolLegacy() *ants.PoolWithFunc {
	return legacyPool
}

func ReleasePoolLegacy() {
	if legacyPool != nil {
		legacyPool.Release()
	}
}
