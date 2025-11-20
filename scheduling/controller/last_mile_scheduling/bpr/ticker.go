package bpr

import (
	"context"
	"database/sql"
	"encoding/json"
	models "scheduling/db_models"

	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// NodePersistentState stores the state of a node that needs to be persisted between BPR runs.
// Now only stores QueueBacklog.
type NodePersistentState struct {
	QueueBacklog float64
}

// Global state managers
var (
	nodeStatesMap      = make(map[string]*NodePersistentState)
	nodeStatesMapMutex sync.RWMutex // Mutex to protect concurrent access to nodeStatesMap
	bprResultsCache    = make(map[string]map[string]int)
	// Mutex to protect concurrent access to bprResultsCache.
	bprResultsMutex = &sync.Mutex{}
)

// GetNodeQueueBacklog retrieves the persistent QueueBacklog for a given IP.
// It returns 0.0 if the IP is not found (default for new nodes).
func GetNodeQueueBacklog(ip string) float64 {
	nodeStatesMapMutex.RLock() // Acquire read lock
	state, found := nodeStatesMap[ip]
	nodeStatesMapMutex.RUnlock() // Release read lock

	if !found {
		log.Infof("No previous state found for IP %s, initializing QueueBacklog to 0.0.", ip)
		return 0.0 // Default for new nodes or if state not yet persisted
	}
	log.Infof("Retrieved previous QueueBacklog for IP %s: %.2f", ip, state.QueueBacklog)
	return state.QueueBacklog
}

// GetAllBPRResults retrieves all stored BPR results from the cache.
// It returns a new map where keys are regionNames and values are copies of their BPR result maps.
// This is to prevent modification of the original cache or its sub-maps by the caller.
func GetAllBPRResults() map[string]map[string]int {
	bprResultsMutex.Lock()
	defer bprResultsMutex.Unlock()

	// Create a new map to hold copies of the results.
	resultsCopy := make(map[string]map[string]int, len(bprResultsCache))

	for region, originalBprMap := range bprResultsCache {
		// For each region, create a copy of its BPR result map.
		if originalBprMap == nil { // Handle case where a nil map might have been stored.
			resultsCopy[region] = nil // Or an empty map: make(map[string]int)
		} else {
			copiedBprMap := make(map[string]int, len(originalBprMap))
			for k, v := range originalBprMap {
				copiedBprMap[k] = v
			}
			resultsCopy[region] = copiedBprMap
		}
	}
	return resultsCopy
}

// UpdateNodeQueueBacklog updates the persistent QueueBacklog for a given IP.
func UpdateNodeQueueBacklog(ip string, newQueueBacklog float64) {
	nodeStatesMapMutex.Lock()         // Acquire write lock
	defer nodeStatesMapMutex.Unlock() // Release write lock

	state, found := nodeStatesMap[ip]
	if !found {
		// If state doesn't exist, create it
		state = &NodePersistentState{}
		nodeStatesMap[ip] = state
	}
	state.QueueBacklog = newQueueBacklog
	log.Infof("Updated persistent QueueBacklog for IP %s to: %.2f", ip, state.QueueBacklog)
}

// ScheduleBPRRuns starts a ticker that runs BPR at specified intervals for a given region.
func ScheduleBPRRuns(ctx context.Context, db *sql.DB, interval time.Duration, region string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	runAndStoreBprResult := func() {
		redistributionProportion, err := models.GetRedistributionProportion(db)
		if err != nil {
			log.Errorf("[ScheduleBPRRuns]: Error fetching redistribution proportion from domain_config table: %v. Skipping this run.", err)
			return
		}
		totalReqIncrement, err := models.GetTotalReqIncrementByRegion(db, region)
		if err != nil {
			log.Errorf("[ScheduleBPRRuns]: Error fetching total request increment for region '%s' during BPR run: %v. Skipping this run.", region, err)
			return
		}
		log.Infof("[ScheduleBPRRuns]: Attempting BPR run for region '%s' with Increment=%d, Proportion=%.2f...",
			region, totalReqIncrement, redistributionProportion)
		bprResultMap, bprErr := Bpr(db, region, totalReqIncrement, redistributionProportion) // bprResultMap is map[string]int
		if bprErr != nil {
			log.Errorf("[ScheduleBPRRuns]: Error during BPR run for region '%s': %v", region, bprErr)
			return
		}
		jsonBytes, err := json.Marshal(bprResultMap)
		if err != nil {
			log.Errorf("[ScheduleBPRRuns]: Failed to marshal map to JSON: %v", err)
		} else {
			log.Infof("[ScheduleBPRRuns]: Result: %s", string(jsonBytes))
		}
		log.Infof("[ScheduleBPRRuns]: Result (map) stored for region '%s'", region)
		bprResultsMutex.Lock()
		bprResultsCache[region] = bprResultMap
		bprResultsMutex.Unlock()

	}
	log.Infof("[ScheduleBPRRuns]: BPR scheduler started for region %s. Interval: %v. Performing initial run...", region, interval)
	runAndStoreBprResult()
	log.Infof("[ScheduleBPRRuns]: Initial BPR run for region '%s' processing done.", region)
	for {
		select {
		case <-ctx.Done():
			log.Infof("[ScheduleBPRRuns]: BPR scheduler for region %s stopping due to context cancellation.", region)
			return
		case <-ticker.C:
			log.Infof("[ScheduleBPRRuns]: Ticker triggered: Starting scheduled BPR run for region '%s'...", region)
			runAndStoreBprResult()
			log.Infof("[ScheduleBPRRuns]: Scheduled BPR run for region '%s' processing done.", region)
		}
	}
}
