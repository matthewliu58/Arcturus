package routing

import (
	"fmt"
	"forwarding/metrics_processing/collector"
	"forwarding/metrics_processing/protocol"
	"forwarding/metrics_processing/storage"

	//_ "forwarding/middle_mile_scheduling/adapter"
	msc "forwarding/middle_mile_scheduling/common"
	ks "forwarding/middle_mile_scheduling/k_shortest"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/panjf2000/ants/v2"
)

type PathManager struct {
	pathChan      chan []msc.PathWithIP
	latestPaths   []msc.PathWithIP
	mu            sync.RWMutex
	sourceIP      string
	destinationIP string
	domain        string                 // Associated domain for this PathManager instance
	params        map[string]interface{} // Algorithm-specific parameters
	calculator    msc.PathCalculator     // Algorithm calculator
	algorithmName string                 // Name of the algorithm in use
}

const (
	// Default algorithm to use when creating new PathManager instances
	DefaultAlgorithm = "k_shortest"

	// Algorithm names
	AlgorithmKShortest = "k_shortest"
	AlgorithmCarousel  = "carousel"
)

var (
	instances     map[string]*PathManager // key: domain, value: PathManager instance
	instancesMu   sync.RWMutex            // Protects instances map
	instancesOnce sync.Once               // Ensures instances map is initialized once

	// Goroutine goroutine_pool for concurrent path calculation
	pathCalculationPool *ants.Pool
	poolOnce            sync.Once

	// Default algorithm configuration (can be updated before GetInstance is called)
	defaultAlgorithm   = DefaultAlgorithm
	defaultAlgorithmMu sync.RWMutex
)

// GetPathCalculationPool returns the singleton goroutine goroutine_pool for path calculations
func GetPathCalculationPool() (*ants.Pool, error) {
	var err error
	poolOnce.Do(func() {
		pathCalculationPool, err = ants.NewPool(100) // Max 100 concurrent metrics_tasks
		if err != nil {
			log.Infof("Failed to create path calculation goroutine_pool: %v", err)
		}
	})
	return pathCalculationPool, err
}

// SetDefaultAlgorithm sets the default algorithm for new PathManager instances
// Should be called before GetInstance is invoked
func SetDefaultAlgorithm(algorithm string) {
	defaultAlgorithmMu.Lock()
	defer defaultAlgorithmMu.Unlock()
	defaultAlgorithm = algorithm
	log.Infof("Default algorithm set to: %s", algorithm)
}

// GetDefaultAlgorithm returns the current default algorithm
func GetDefaultAlgorithm() string {
	defaultAlgorithmMu.RLock()
	defer defaultAlgorithmMu.RUnlock()
	return defaultAlgorithm
}

// fileManagerInstance is the FileManager instance used by the current application
var (
	fileManagerInstance *storage.FileManager
	fileManagerOnce     sync.Once
)

// getFileManager retrieves the singleton instance of FileManager
func getFileManager() *storage.FileManager {
	//if fileManagerInstance != nil {
	//	return fileManagerInstance
	//}
	//fileManagerOnce.Do(func() {
	var err error
	fileManagerInstance, err = storage.NewFileManager("../../agent_storage")
	if err != nil {
		//fileManagerInstance = nil
		log.Warnf("Failed to create FileManager: %v", err)
		return nil
	}
	//})
	return fileManagerInstance
}

// GetTargetIPByDomain looks up the corresponding target IP from the domain mapping
func GetTargetIPByDomain(domain string) string {
	// Get the FileManager instance
	fileManager := getFileManager()
	if fileManager == nil {
		log.Warnf("Failed to retrieve FileManager instance")
		return ""
	}

	// Get all domain mappings
	mappings := fileManager.GetDomainIPMappings()
	if mappings == nil {
		log.Warningf("Domain mappings are empty")
		return ""
	}

	// Find the matching domain
	for _, mapping := range mappings {
		if mapping.Domain == domain {
			log.Infof("Found mapped IP for domain %s: %s", domain, mapping.Ip)
			return mapping.Ip
		}
	}

	// Return empty string if no mapping is found
	log.Infof("No mapping found for domain %s", domain)
	return ""
}

// GetDefaultTargetIP retrieves the default target IP
// Returns the IP of the first record in the domain mapping
// Returns an empty string if there are no mapping records
func GetDefaultTargetIP() string {
	// Get the FileManager instance
	fileManager := getFileManager()
	if fileManager == nil {
		log.Infof("Failed to retrieve FileManager instance")
		return ""
	}

	// Get all domain mappings
	mappings := fileManager.GetDomainIPMappings()

	// If there are mapping records, return the IP of the first record
	if mappings != nil && len(mappings) > 0 {
		defaultIP := mappings[0].Ip
		log.Infof("Using IP from the first domain mapping: %s as default target", defaultIP)
		return defaultIP
	}

	// Return empty string if there are no mapping records
	log.Infof("Domain mappings are empty, no default target IP available")
	return ""
}

// GetAllDomainMapIP get all map ip list
func GetAllDomainMapIP() []*protocol.DomainIPMapping {
	// Get the FileManager instance
	fileManager := getFileManager()
	if fileManager == nil {
		log.Warnf("Failed to retrieve FileManager instance")
		return []*protocol.DomainIPMapping{}
	}
	// Get all domain mappings
	mappings := fileManager.GetDomainIPMappings()
	return mappings
}

// GetInstance retrieves the PathManager instance for the specified domain
// If domain is empty, returns the PathManager for the first domain mapping (default)
// Creates a new instance if one doesn't exist for the domain
func GetInstance(domain string) *PathManager {
	// Initialize instances map once
	instancesOnce.Do(func() {
		instances = make(map[string]*PathManager)
		log.Infof("PathManager multi-instance map initialized")
	})

	// If domain is empty, use the default domain
	if domain == "" {
		domain = getDefaultDomain()
		if domain == "" {
			log.Infof("GetInstance: no default domain available, returning nil")
			return nil
		}
	}

	// Try to get existing instance (read lock)
	instancesMu.RLock()
	pm, exists := instances[domain]
	instancesMu.RUnlock()

	if exists {
		log.Infof("find instance for %v", domain)
		return pm
	}

	// Instance doesn't exist, need to create (write lock)
	instancesMu.Lock()
	defer instancesMu.Unlock()

	// Double-check after acquiring write lock
	if pm, exists := instances[domain]; exists {
		log.Infof("double find instance for %v", domain)
		return pm
	}

	// Get source IP
	sourceIP, err := collector.GetIP()
	if err != nil {
		log.Infof("GetInstance: failed to get source IP for domain %s: %v", domain, err)
		return nil
	}

	// Get target IP for this domain
	targetIP := GetTargetIPByDomain(domain)
	if targetIP == "" {
		log.Infof("GetInstance: no target IP found for domain %s", domain)
		return nil
	}

	// Create new PathManager instance
	pm = &PathManager{
		pathChan:      make(chan []msc.PathWithIP, 1),
		latestPaths:   nil,
		sourceIP:      sourceIP,
		destinationIP: targetIP,
		domain:        domain,
		params: map[string]interface{}{
			"k": 2, // Default k value for K-Shortest
		},
		algorithmName: GetDefaultAlgorithm(), // Use configured default algorithm
	}

	// Initialize the algorithm calculator with the default algorithm
	algorithmName := GetDefaultAlgorithm()
	calc, err := msc.GetGlobal(algorithmName)
	if err != nil {
		log.Warnf("GetInstance: failed to get algorithm '%s' for domain %s: %v, will fallback to %s",
			algorithmName, domain, err, AlgorithmKShortest)

		// Fallback to k_shortest if default algorithm is not available
		calc, err = msc.GetGlobal(AlgorithmKShortest)
		if err != nil {
			log.Infof("GetInstance: fallback to k_shortest also failed for domain %s: %v", domain, err)
			pm.calculator = nil
			pm.algorithmName = AlgorithmKShortest
		} else {
			pm.calculator = calc
			pm.algorithmName = AlgorithmKShortest
		}
	} else {
		pm.calculator = calc
	}

	// Start path listener goroutine
	go pm.pathListener()

	// Store in map
	instances[domain] = pm
	log.Infof("GetInstance: created new PathManager for domain %s (target IP: %s, algorithm: %s)", domain, targetIP, pm.algorithmName)

	return pm
}

// getDefaultDomain returns the domain of the first domain mapping
func getDefaultDomain() string {
	mappings := GetAllDomainMapIP()
	if len(mappings) > 0 {
		return mappings[0].Domain
	}
	return ""
}

// RemoveInstance removes and cleans up a PathManager instance for a specific domain
func RemoveInstance(domain string) {
	instancesMu.Lock()
	defer instancesMu.Unlock()

	if pm, exists := instances[domain]; exists {
		// Close the pathChan to stop the pathListener goroutine
		close(pm.pathChan)

		// Remove from map
		delete(instances, domain)

		log.Infof("RemoveInstance: removed PathManager for domain %s", domain)
	} else {
		log.Warningf("RemoveInstance: no PathManager found for domain %s", domain)
	}
}

func (pm *PathManager) pathListener() {
	for paths := range pm.pathChan {
		pm.mu.Lock()
		pm.latestPaths = paths
		pm.mu.Unlock()
		log.Infof(" %d ", len(paths))
	}
}

// SetAlgorithm changes the algorithm used by this PathManager
func (pm *PathManager) SetAlgorithm(algorithmName string) error {
	calc, err := msc.GetGlobal(algorithmName)
	if err != nil {
		log.Infof("SetAlgorithm: failed to get algorithm '%s' for domain %s: %v", algorithmName, pm.domain, err)
		return err
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.calculator = calc
	pm.algorithmName = algorithmName
	log.Infof("SetAlgorithm: changed algorithm to '%s' for domain %s", algorithmName, pm.domain)
	return nil
}

// CalculatePaths calculates paths using the configured algorithm
func (pm *PathManager) CalculatePaths(network msc.Network,
	ipToIndex map[string]int,
	indexToIP map[int]string) {

	sourceIdx, srcExists := ipToIndex[pm.sourceIP]
	destIdx, destExists := ipToIndex[pm.destinationIP]

	if !srcExists || !destExists {
		log.Warningf("CalculatePaths: domain=%s, source or destination IP not in mapping (srcExists=%v, destExists=%v, sourceIP=%s, destinationIP=%s)",
			pm.domain, srcExists, destExists, pm.sourceIP, pm.destinationIP)
		return
	}

	// Get or use default calculator
	pm.mu.RLock()
	calculator := pm.calculator
	algorithmName := pm.algorithmName
	pm.mu.RUnlock()

	if calculator == nil {
		log.Warnf("CalculatePaths: domain=%s, no calculator available (algorithmName=%s), using fallback K-Shortest algorithm",
			pm.domain, algorithmName)
		// Fallback to K-Shortest if no calculator is set
		pm.calculatePathsKShortest(network, sourceIdx, destIdx, ipToIndex, indexToIP)
		return
	}

	// Use the algorithm calculator
	// Build parameters map for the algorithm
	pm.mu.RLock()
	params := pm.params
	pm.mu.RUnlock()

	if params == nil {
		params = make(map[string]interface{})
	}

	log.Infof("CalculatePaths: domain=%s, using algorithm=%s, params=%v (sourceIdx=%d destIdx=%d)", pm.domain, algorithmName, params, sourceIdx, destIdx)

	pathsWithIP := calculator.ComputePaths(&network, sourceIdx, destIdx, params, ipToIndex, indexToIP)

	log.Infof("CalculatePaths: domain=%s, algorithm=%s completed, computed %d paths (sourceIP=%s destIP=%s)",
		pm.domain, algorithmName, len(pathsWithIP), pm.sourceIP, pm.destinationIP)

	// Print each path detail for debugging
	for i, p := range pathsWithIP {
		pathNodes := fmt.Sprintf("%v", p.IPList)
		log.Infof("CalculatePaths: domain=%s, path[%d] nodes=%s, latency=%d, weight=%d",
			pm.domain, i, pathNodes, p.Latency, p.Weight)
	}

	// Send paths directly to channel
	select {
	case pm.pathChan <- pathsWithIP:
	default:
		// Channel full, discard old message and send new one
		select {
		case <-pm.pathChan:
		default:
		}
		pm.pathChan <- pathsWithIP
	}
}

// calculatePathsKShortest is a fallback method using K-Shortest algorithm directly
func (pm *PathManager) calculatePathsKShortest(network msc.Network,
	sourceIdx, destIdx int,
	ipToIndex map[string]int,
	indexToIP map[int]string) {

	// Extract k from params, default to 2
	pm.mu.RLock()
	params := pm.params
	pm.mu.RUnlock()

	k := 2 // Default value
	if params != nil {
		if kVal, exists := params["k"]; exists {
			switch v := kVal.(type) {
			case int:
				k = v
			case int32:
				k = int(v)
			case int64:
				k = int(v)
			case float64:
				k = int(v)
			}
		}
	}

	log.Infof("calculatePathsKShortest: domain=%s, sourceIP=%s(sourceIdx=%d), destIP=%s(destIdx=%d), k=%d",
		pm.domain, pm.sourceIP, sourceIdx, pm.destinationIP, destIdx, k)

	flow := msc.Flow{Source: sourceIdx, Destination: destIdx}
	paths := ks.KShortest(network, flow, k, 3, 2)

	log.Infof("calculatePathsKShortest: domain=%s, k_shortest returned %d raw paths", pm.domain, len(paths))

	var pathsWithIP []msc.PathWithIP
	var totalLatency int
	for _, p := range paths {
		totalLatency += p.Latency
	}

	if len(paths) == 0 || totalLatency == 0 {
		log.Warningf("calculatePathsKShortest: domain=%s, no valid paths found (count=%d, totalLatency=%d)", pm.domain, len(paths), totalLatency)
		select {
		case pm.pathChan <- []msc.PathWithIP{}:
		default:
		}
		return
	}

	for idx, p := range paths {
		ipNodes := make([]string, len(p.Nodes))
		for j, node := range p.Nodes {
			ipNodes[j] = indexToIP[node]
		}
		var weight int
		if p.Latency == 0 {
			weight = 100
		} else {
			weight = totalLatency / p.Latency
		}
		pathsWithIP = append(pathsWithIP, msc.PathWithIP{
			IPList:  ipNodes,
			Latency: p.Latency,
			Weight:  weight,
		})

		// Print detailed path information for debugging
		pathNodes := fmt.Sprintf("%v", ipNodes)
		log.Infof("calculatePathsKShortest: domain=%s, path[%d] nodes=%s, latency=%d, weight=%d",
			pm.domain, idx, pathNodes, p.Latency, weight)
	}

	log.Infof("calculatePathsKShortest: domain=%s, produced %d paths (totalLatency=%d)", pm.domain, len(pathsWithIP), totalLatency)

	select {
	case pm.pathChan <- pathsWithIP:
	default:
		<-pm.pathChan
		pm.pathChan <- pathsWithIP
	}
}

func (pm *PathManager) GetPaths() []msc.PathWithIP {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.latestPaths == nil {
		return []msc.PathWithIP{}
	}
	paths := make([]msc.PathWithIP, len(pm.latestPaths))
	copy(paths, pm.latestPaths)
	return paths
}

func (pm *PathManager) SetSourceDestination(source, destination string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.sourceIP = source
	pm.destinationIP = destination
}

// CalculatePathsForAllDomains calculates paths for all domain PathManager instances
// This should be called when the network topology is updated
// Uses goroutine goroutine_pool for concurrent path calculation
func CalculatePathsForAllDomains(network msc.Network,
	ipToIndex map[string]int, indexToIP map[int]string) {

	instancesMu.RLock()
	domains := make([]string, 0, len(instances))
	managers := make([]*PathManager, 0, len(instances))
	for domain, pm := range instances {
		domains = append(domains, domain)
		managers = append(managers, pm)
	}
	instancesMu.RUnlock()

	if len(managers) == 0 {
		log.Warnf("CalculatePathsForAllDomains: no PathManager instances exist yet")
		return
	}

	log.Infof("CalculatePathsForAllDomains: calculating paths for %d domains using goroutine goroutine_pool", len(managers))

	// Get or create the path calculation goroutine_pool
	pool, err := GetPathCalculationPool()
	if err != nil {
		log.Infof("CalculatePathsForAllDomains: failed to get goroutine_pool: %v, falling back to sequential calculation", err)
		// Fallback to sequential calculation
		for i, pm := range managers {
			log.Infof("CalculatePathsForAllDomains: calculating for domain %s (target: %s)",
				domains[i], pm.destinationIP)
			pm.CalculatePaths(network, ipToIndex, indexToIP)
		}
		return
	}

	// Submit metrics_tasks to the goroutine goroutine_pool
	var wg sync.WaitGroup
	for i, pm := range managers {
		wg.Add(1)
		domain := domains[i]

		// Capture variables for closure
		pathMgr := pm
		domainName := domain

		// Submit the task to the goroutine_pool
		err := pool.Submit(func() {
			defer wg.Done()
			log.Infof("CalculatePathsForAllDomains: [goroutine goroutine_pool] calculating for domain %s (target: %s)",
				domainName, pathMgr.destinationIP)
			pathMgr.CalculatePaths(network, ipToIndex, indexToIP)
		})

		if err != nil {
			log.Infof("CalculatePathsForAllDomains: failed to submit task for domain %s: %v", domain, err)
			wg.Done() // Manually decrement since we won't execute
		}
	}

	// Wait for all metrics_tasks to complete
	wg.Wait()
	log.Infof("CalculatePathsForAllDomains: completed path calculation for all %d domains", len(managers))
}

// PreloadDomains pre-creates PathManager instances for all configured domains
// This can be called during initialization to avoid lazy-loading delays
func PreloadDomains() {
	mappings := GetAllDomainMapIP()
	if len(mappings) == 0 {
		log.Println("PreloadDomains: no domain mappings found")
		return
	}

	log.Infof("PreloadDomains: preloading %d domains", len(mappings))

	for _, mapping := range mappings {
		pm := GetInstance(mapping.Domain)
		if pm != nil {
			log.Infof("PreloadDomains: preloaded domain %s", mapping.Domain)
		} else {
			log.Infof("PreloadDomains: failed to preload domain %s", mapping.Domain)
		}
	}
}
