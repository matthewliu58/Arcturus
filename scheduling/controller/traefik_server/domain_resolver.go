package traefik_server

import (
	log "github.com/sirupsen/logrus"
	"scheduling/controller/last_mile_scheduling/bpr"
)

// GetDomainTargets returns a list of IP addresses and their weights for the provided domain.
// It filters out targets with non-positive weights.
// Returns an empty, non-nil slice if the domain is not found, has no BPR data,
// or all its targets have non-positive weights.
func GetDomainTargets(domain string) []TargetEntry {
	allBprData := bpr.GetAllBPRResults() // Get all raw data from the bpr package

	// Get the BPR data specific to the given domain
	domainSpecificBpr, ok := allBprData[domain]
	if !ok {
		log.Infof("Info: Domain '%s' not found in BPR results. Returning empty target list.", domain)
		return make([]TargetEntry, 0)
	}

	// If the BPR data for the domain is nil or empty, return an empty TargetEntry slice
	if domainSpecificBpr == nil || len(domainSpecificBpr) == 0 {
		log.Infof("Info: Domain '%s' has nil or empty BPR data. Returning empty target list.", domain)
		return make([]TargetEntry, 0)
	}

	// Pre-allocate capacity based on the original length, but will filter when adding
	filteredTargets := make([]TargetEntry, 0, len(domainSpecificBpr))

	// Iterate through the domain-specific BPR data and only add targets with positive weights
	for ip, weight := range domainSpecificBpr {
		if weight > 0 { // Only add targets with positive weights
			filteredTargets = append(filteredTargets, TargetEntry{
				IP:     ip,
				Weight: weight,
			})
		} else {
			log.Debugf("Debug: For domain '%s', skipping target IP %s due to non-positive weight %d", domain, ip, weight)
		}
	}

	// If the filtered list is empty after checking weights
	if len(filteredTargets) == 0 {
		log.Infof("Info: For domain '%s', all targets had non-positive weights. Returning empty target list.", domain)
		// No need to return again, filteredTargets is already empty and will be returned
	}

	// log.Printf("Debug: GetDomainTargets for domain '%s': returning filtered targets = %#v\n", domain, filteredTargets)
	return filteredTargets
}
