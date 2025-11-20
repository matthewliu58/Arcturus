package linkevaluate

import (
	"database/sql"
	log "github.com/sirupsen/logrus"
	"scheduling/db_models"
	"scheduling/structs"
)

const (
	ThresholdCpuMean float64 = 50
	ThresholdCpuVar  float64 = 50
	Weight           float64 = 1
)

// IsTargetServerIP checks if the given IP is a target server IP by querying the database
func IsTargetServerIP(db *sql.DB, ip string) bool {
	query := "SELECT COUNT(*) FROM domain_origin WHERE origin_ip = ?"
	var count int
	err := db.QueryRow(query, ip).Scan(&count)
	if err != nil {
		log.Errorf("Error querying target server IP: %v", err)
		return false
	}
	return count > 0
}

// CalculateLinkWeight calculates the weight value for a link between two IPs
// For target servers (without forwarding module), only delay is used as weight
// For normal nodes, a combination of delay and CPU metrics_processing is used
func CalculateLinkWeight(db *sql.DB, ip1 string, ip2 string, net structs.NetState) structs.Result {
	// Query delay data
	avgDelay, err := db_models.GetDelay(db, ip1, ip2)
	if err != nil {
		log.Errorf("Failed to query IP delay: %v", err)
		return structs.Result{}
	}

	// Check if ip2 is a target server IP
	if IsTargetServerIP(db, ip2) {
		log.Infof("Target IP %s is a destination server, using only delay as weight", ip2)

		// Return result with delay as the only weight factor
		return structs.Result{
			Ip1:   ip1,
			Ip2:   ip2,
			Value: avgDelay * Weight, // Only use delay * weight coefficient as final value
		}
	}

	// If not a target server, use the original three-factor calculation logic
	stat, err := db_models.GetCpuStats(db, ip2)
	if err != nil {
		log.Errorf("Failed to get CPU usage for %s: %v", ip2, err)
		return structs.Result{}
	}

	// Original calculation logic follows
	node := structs.NodeState{
		CpuMean: stat.Mean,
		CpuVar:  stat.Variance,
	}

	params := SystemParams{
		ThresholdCpuMean: ThresholdCpuMean,
		ThresholdCpuVar:  ThresholdCpuVar,
		Weight:           Weight,
	}

	normalCpuMean, normalCpuVar := params.Normalize(&node, &net)
	QMean, QVar, err := db_models.QueryVirtualQueueCPUByIP(db, ip1, ip2)
	if err != nil {
		log.Errorf("Failed to query virtual CPU queue: %v", err)
	}

	e := Evaluation{
		Delay:         avgDelay,
		NormalCpuMean: normalCpuMean,
		NormalCpuVar:  normalCpuVar,
		QMean:         QMean,
		QVar:          QVar,
		Params:        params,
		State:         net,
	}

	VirtualQueueCPUMean := e.UpdateQMean()
	VirtualQueueCPUVariance := e.UpdateQVar()
	finalValue := e.DriftPlusPenalty()

	err = db_models.UpdateVirtualQueueAndCPUMetrics(db, ip1, ip2, avgDelay, stat.Mean, stat.Variance, VirtualQueueCPUMean, VirtualQueueCPUVariance)
	if err != nil {
		return structs.Result{}
	}

	res := structs.Result{
		Ip1:   ip1,
		Ip2:   ip2,
		Value: finalValue,
	}
	return res
}
