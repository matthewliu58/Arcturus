package routing

import (
	"sync"

	log "github.com/sirupsen/logrus"

	logrus "github.com/sirupsen/logrus"
)

// Global PathManagerPool instance for ACCESS to use
var (
	globalPoolInstance *PathManagerPool
	globalPoolMutex    sync.RWMutex
)

// SetPathManagerPoolInstance sets the global goroutine_pool instance
func SetPathManagerPoolInstance(pool *PathManagerPool) {
	globalPoolMutex.Lock()
	defer globalPoolMutex.Unlock()
	globalPoolInstance = pool
	logrus.Info("Global PathManagerPool instance set")
}

// GetPathManagerPoolInstance returns the global goroutine_pool instance
func GetPathManagerPoolInstance() *PathManagerPool {
	globalPoolMutex.RLock()
	defer globalPoolMutex.RUnlock()
	return globalPoolInstance
}

// DomainConfig represents configuration for a single domain
type DomainConfig struct {
	Name          string                 // Domain name identifier
	SourceIP      string                 // Source IP address
	DestinationIP string                 // Destination IP address
	Algorithm     string                 // Routing algorithm to use
	Params        map[string]interface{} // Algorithm-specific parameters
}

// PathManagerPool manages multiple PathManagers for different domains
// Supports concurrent path calculation for multiple destinations
type PathManagerPool struct {
	managers map[string]*PathManager // key: domain name
	mu       sync.RWMutex
}

// NewPathManagerPool creates a new PathManagerPool
func NewPathManagerPool() *PathManagerPool {
	return &PathManagerPool{
		managers: make(map[string]*PathManager),
	}
}

// AddDomain registers a new domain with its PathManager
func (pool *PathManagerPool) AddDomain(config DomainConfig) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	// Get existing or create new PathManager for this domain
	pm := GetInstance(config.Name)
	if pm == nil {
		log.Infof("Failed to get PathManager instance for domain '%s'", config.Name)
		return nil
	}

	// Set algorithm if specified
	if config.Algorithm != "" {
		if err := pm.SetAlgorithm(config.Algorithm); err != nil {
			logrus.Warnf("Failed to set algorithm '%s' for domain '%s': %v",
				config.Algorithm, config.Name, err)
		}
	}

	pool.managers[config.Name] = pm

	logrus.Infof("Domain registered: '%s' (%s -> %s) using %s algorithm",
		config.Name, pm.sourceIP, pm.destinationIP, pm.algorithmName)

	return nil
}

// GetManager retrieves a PathManager by domain name
func (pool *PathManagerPool) GetManager(domain string) (*PathManager, bool) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	pm, exists := pool.managers[domain]
	return pm, exists
}

// GetManagerByDestination retrieves a PathManager by destination IP
// This is useful when the domain name is not known but we have the destination IP
func (pool *PathManagerPool) GetManagerByDestination(destIP string) (*PathManager, string, bool) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	for domain, pm := range pool.managers {
		if pm.destinationIP == destIP {
			return pm, domain, true
		}
	}
	return nil, "", false
}

// GetAllManagers returns a copy of all PathManagers
// Safe for concurrent iteration
func (pool *PathManagerPool) GetAllManagers() map[string]*PathManager {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	managers := make(map[string]*PathManager, len(pool.managers))
	for k, v := range pool.managers {
		managers[k] = v
	}
	return managers
}

// RemoveDomain removes a domain from the goroutine_pool
func (pool *PathManagerPool) RemoveDomain(domain string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	if _, exists := pool.managers[domain]; exists {
		delete(pool.managers, domain)
		logrus.Infof("Domain removed: '%s'", domain)
	}
}

// GetDomainCount returns the number of registered domains
func (pool *PathManagerPool) GetDomainCount() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return len(pool.managers)
}

// ListDomains returns a list of all registered domain names
func (pool *PathManagerPool) ListDomains() []string {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	domains := make([]string, 0, len(pool.managers))
	for domain := range pool.managers {
		domains = append(domains, domain)
	}
	return domains
}

// GetDomainInfo returns configuration information for a domain
func (pool *PathManagerPool) GetDomainInfo(domain string) (DomainConfig, bool) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	pm, exists := pool.managers[domain]
	if !exists {
		return DomainConfig{}, false
	}

	config := DomainConfig{
		Name:          domain,
		SourceIP:      pm.sourceIP,
		DestinationIP: pm.destinationIP,
		Algorithm:     pm.algorithmName,
		Params:        map[string]interface{}{},
	}

	return config, true
}

// UpdateDomainAlgorithm updates the algorithm for a specific domain
func (pool *PathManagerPool) UpdateDomainAlgorithm(domain, algorithm string) error {
	pool.mu.RLock()
	pm, exists := pool.managers[domain]
	pool.mu.RUnlock()

	if !exists {
		logrus.Warnf("Domain '%s' not found in goroutine_pool", domain)
		return nil
	}

	return pm.SetAlgorithm(algorithm)
}
