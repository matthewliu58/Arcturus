package connection

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"net"
	"sync"
	"time"
)

var DefaultPoolConfig = PoolConfig{
	InitialCap:     5,
	MaxCap:         10,
	AcquireTimeout: 5 * time.Second,
}

var (
	// key(IP:Port)，value
	poolMap = make(map[string]*Pool)
	poolMu  sync.RWMutex // poolMap
)

var CreateConnectionFactory = func(addr string) Factory {
	return func() (net.Conn, error) {
		return net.Dial("tcp", addr)
	}
}

func GetOrCreatePool(targetAddr string, config ...PoolConfig) (*Pool, error) {

	poolConfig := DefaultPoolConfig
	if len(config) > 0 {
		poolConfig = config[0]
	}

	poolMu.RLock()
	pool, exists := poolMap[targetAddr]
	poolMu.RUnlock()

	if exists {
		return pool, nil
	}

	poolMu.Lock()
	defer poolMu.Unlock()

	if pool, exists = poolMap[targetAddr]; exists {
		return pool, nil
	}

	factory := CreateConnectionFactory(targetAddr)

	newPool, err := NewChannelPool(poolConfig, factory)
	if err != nil {
		return nil, fmt.Errorf(": %v", err)
	}

	poolMap[targetAddr] = &newPool
	log.Infof(" %s ", targetAddr)

	return &newPool, nil
}
