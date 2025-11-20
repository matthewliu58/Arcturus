package db_models

import (
	"database/sql"
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"math"
	pb "scheduling/controller/metrics_processing/protocol"
	"scheduling/structs"
	"sort"
	"strconv"
	"time"

	"github.com/gomodule/redigo/redis"
)

// NodeSystemInfo struct is used to store the results queried from the database
type NodeSystemInfo struct {
	IP        string    `json:"ip"`        // IP Address
	CPUCores  int       `json:"cpu_cores"` // Number of CPU Cores
	CPUUsage  float64   `json:"cpu_usage"` // CPU Usage percentage
	Timestamp time.Time `json:"timestamp"` // Timestamp of the record
}

func QueryIp(db *sql.DB) ([]string, error) {
	rows, err := db.Query("SELECT DISTINCT ip FROM node_region")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ips []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			log.Errorf("QueryIp Scan ip failed, err=%s", err)
			return nil, err
		}
		ips = append(ips, ip)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ips, nil
}

func CalculateAvgDelay(conn redis.Conn, db *sql.DB, ip1 string, ip2 string) {
	var totalDelay float64
	totalDelay = 0
	key := fmt.Sprintf("%s:%s", ip1, ip2)

	values, err := redis.Values(conn.Do("LRANGE", key, -10, -1))
	if err != nil {
		log.Fatalf("Failed to retrieve data from Redis: %v", err)
	}

	if len(values) == 0 {
		log.Infof("No data found for key: %s", key)
		return
	}

	for _, value := range values {
		var result structs.ProbeResult
		err := json.Unmarshal(value.([]byte), &result)
		if err != nil {
			log.Errorf("Failed to parse Redis value: %v", err)
			continue
		}
		totalDelay += float64(result.Delay)
	}

	avgDelay := totalDelay / float64(len(values))

	_ = InsertLinkInfo(db, ip1, ip2, avgDelay, time.Now().Format("2006-01-02 15:04:05"))
}

func GetDelay(db *sql.DB, ip1 string, ip2 string) (float64, error) {
	var delay float64

	query := `
		SELECT tcp_delay
		FROM region_probe_info 
		WHERE source_ip = ? AND target_ip = ? 
		ORDER BY probe_time DESC 
		LIMIT 1;
	`
	err := db.QueryRow(query, ip1, ip2).Scan(&delay)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("no data found for %s->%s", ip1, ip2)
		}
		return 0, fmt.Errorf("failed to query delay: %v", err)
	}
	return delay, nil
}

func GetCpuStats(db *sql.DB, destinationIP string) (*structs.CPUStats, error) {

	query := `
		SELECT cpu_usage
		FROM system_info
		WHERE ip = ?
		ORDER BY created_at DESC
		LIMIT 10;
	`

	rows, err := db.Query(query, destinationIP)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %v", err)
	}
	defer rows.Close()
	var (
		cpuUsages   []float64 //
		sum         float64   //  (mean)
		varianceSum float64   //  (variance)
	)

	for rows.Next() {
		var cpuUsage float64
		if err := rows.Scan(&cpuUsage); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		cpuUsages = append(cpuUsages, cpuUsage)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %v", err)
	}

	if len(cpuUsages) == 0 {
		return nil, fmt.Errorf("no CPU usage data found for device %s", destinationIP)
	}

	for _, usage := range cpuUsages {
		sum += usage
	}
	mean := sum / float64(len(cpuUsages))

	for _, usage := range cpuUsages {
		varianceSum += math.Pow(usage-mean, 2)
	}
	variance := varianceSum / float64(len(cpuUsages))

	stats := &structs.CPUStats{
		DestinationIP: destinationIP,
		Mean:          mean,
		Variance:      variance,
	}
	return stats, nil
}

func QueryVirtualQueueCPUByIP(db *sql.DB, sourceIP string, destinationIP string) (float64, float64, error) {

	query := `
		SELECT 
			virtual_queue_cpu_mean,
			virtual_queue_cpu_variance
		FROM 
			network_metrics
		WHERE 
			source_ip = ? 
			AND destination_ip = ? 
		ORDER BY 
			updated_at DESC 
		LIMIT 1;
	`

	var virtualQueueCPUMean float64
	var virtualQueueCPUVariance float64

	err := db.QueryRow(query, sourceIP, destinationIP).Scan(&virtualQueueCPUMean, &virtualQueueCPUVariance)
	if err != nil {
		if err == sql.ErrNoRows {

			return 0, 0, fmt.Errorf("no records found for sourceIP to destinationIP: %s:%s", sourceIP, destinationIP)
		}

		return 0, 0, fmt.Errorf("error querying database: %w", err)
	}

	return virtualQueueCPUMean, virtualQueueCPUVariance, nil
}

func GetCPUPerformanceList(db *sql.DB, thresholdCpuMean float64, thresholdCpuVar float64) (aboveCpuMeans, belowCpuMeans, aboveCpuVars, belowCpuVars []float64, err error) {

	query := `
	WITH RankedRecords AS (
		SELECT 
			ip,
			cpu_usage,
			ROW_NUMBER() OVER (PARTITION BY ip ORDER BY timestamp DESC) AS rn
		FROM system_info
	)
	SELECT 
		ip,
		AVG(cpu_usage) AS avg_cpu_usage,
		VARIANCE(cpu_usage) AS variance_cpu_usage
	FROM RankedRecords
	WHERE rn <= 10
	GROUP BY ip;
	`

	rows, queryErr := db.Query(query)
	if queryErr != nil {
		return nil, nil, nil, nil, queryErr
	}
	defer rows.Close()

	for rows.Next() {
		var (
			ip               string
			avgCpuUsage      float64
			varianceCpuUsage float64
		)

		scanErr := rows.Scan(&ip, &avgCpuUsage, &varianceCpuUsage)
		if scanErr != nil {
			return nil, nil, nil, nil, scanErr
		}

		if avgCpuUsage > thresholdCpuMean {
			aboveCpuMeans = append(aboveCpuMeans, avgCpuUsage)
		} else {
			belowCpuMeans = append(belowCpuMeans, avgCpuUsage)
		}

		if varianceCpuUsage > thresholdCpuVar {
			aboveCpuVars = append(aboveCpuVars, varianceCpuUsage)
		} else {
			belowCpuVars = append(belowCpuVars, varianceCpuUsage)
		}
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, nil, nil, nil, rowsErr
	}

	sort.Float64s(aboveCpuMeans)
	sort.Float64s(belowCpuMeans)
	sort.Float64s(aboveCpuVars)
	sort.Float64s(belowCpuVars)
	return aboveCpuMeans, belowCpuMeans, aboveCpuVars, belowCpuVars, nil
}

func QueryOriginIP(db *sql.DB, domain string) (string, error) {
	var originIP string

	err := db.QueryRow("SELECT origin_ip FROM domain_origin WHERE domain = ?", domain).Scan(&originIP)
	if err != nil {
		if err == sql.ErrNoRows {
			// ，
			return "", fmt.Errorf("domain '%s' not found", domain)
		}

		return "", fmt.Errorf("failed to query database for domain '%s': %v", domain, err)
	}

	return originIP, nil
}

func QueryNodeInfo(db *sql.DB) ([]*pb.NodeInfo, error) {
	// 只查询description中包含"forwarding"的节点
	rows, err := db.Query("SELECT ip, region FROM node_region WHERE description LIKE '%forwarding%'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*pb.NodeInfo
	for rows.Next() {
		var node pb.NodeInfo
		if err := rows.Scan(&node.Ip, &node.Region); err != nil {
			return nil, err
		}
		nodes = append(nodes, &node)
	}

	return nodes, nil
}

func QueryDomainIPMappings(db *sql.DB) ([]*pb.DomainIPMapping, error) {

	rows, err := db.Query("SELECT domain, origin_ip FROM domain_origin")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mappings []*pb.DomainIPMapping
	for rows.Next() {
		var mapping pb.DomainIPMapping
		if err := rows.Scan(&mapping.Domain, &mapping.Ip); err != nil {
			return nil, err
		}
		mappings = append(mappings, &mapping)
	}

	return mappings, nil
}

func GetNodeRegion(db *sql.DB, ip string) (string, error) {
	var region string
	query := "SELECT region FROM node_region WHERE ip = ?"
	err := db.QueryRow(query, ip).Scan(&region)
	if err != nil {
		if err == sql.ErrNoRows {
			return "unknown", nil
		}
		return "", err
	}
	return region, nil
}

func GetAllRegions(db *sql.DB) ([]string, error) {
	var regions []string

	rows, err := db.Query("SELECT DISTINCT region FROM node_region")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var region string
		if err := rows.Scan(&region); err != nil {
			return nil, err
		}
		regions = append(regions, region)
	}

	return regions, nil
}

func GetRegionIPs(db *sql.DB, region string) ([]string, error) {
	// 只查询description中包含"forwarding"的节点IP
	rows, err := db.Query("SELECT ip FROM node_region WHERE region = ? AND description LIKE '%forwarding%'", region)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ips []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		ips = append(ips, ip)
	}

	return ips, nil
}

func CountMetricsNodes(db *sql.DB) (int, error) {

	query := `SELECT COUNT(DISTINCT ip) FROM system_info`

	var count int
	err := db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf(": %w", err)
	}

	return count, nil
}

func GetMedianVirtual(db *sql.DB) (float64, float64, error) {
	query := `
	WITH latest_records AS (
		SELECT *
		FROM (
			SELECT *,
				   ROW_NUMBER() OVER (PARTITION BY source_ip, destination_ip ORDER BY updated_at DESC) as rn
			FROM network_metrics
		) ranked
		WHERE rn = 1
	)
	SELECT 
		PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY virtual_queue_cpu_mean) OVER () as median_virtual_queue_cpu_mean,
		PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY virtual_queue_cpu_variance) OVER () as median_virtual_queue_cpu_variance
	FROM latest_records
	LIMIT 1;`

	row := db.QueryRow(query)

	var medianMean, medianVariance float64

	err := row.Scan(&medianMean, &medianVariance)
	if err != nil {

		if err == sql.ErrNoRows {
			return 0, 0, fmt.Errorf("")
		}
		return 0, 0, fmt.Errorf(": %w", err)
	}

	return medianMean, medianVariance, nil
}

// GetLatestSystemInfoByRegion method queries the latest node system information for a given region.
// It no longer fetches the region itself in the result struct.
func GetLatestNodeInfoByRegion(db *sql.DB, region string) ([]NodeSystemInfo, error) {
	// SQL query to get the latest system info for IPs within a specific region.
	// The 'nr.region' column is removed from the SELECT list.
	query := `
        WITH RankedSystemInfo AS (
            SELECT
                ip,
                cpu_cores,
                cpu_usage,
                timestamp, -- Ensure the timestamp column in system_info is DATETIME or TIMESTAMP type
                ROW_NUMBER() OVER (PARTITION BY ip ORDER BY timestamp DESC) as rn
            FROM
                system_info
        )
        SELECT
            rsi.ip,
            rsi.cpu_cores,
            rsi.cpu_usage,
            rsi.timestamp
        FROM
            node_region nr
        INNER JOIN
            RankedSystemInfo rsi ON nr.ip = rsi.ip
        WHERE
            nr.region = ? 
            AND rsi.rn = 1;
    `

	rows, err := db.Query(query, region)
	if err != nil {
		return nil, fmt.Errorf("error querying latest system info by region '%s': %w", region, err)
	}
	defer rows.Close()

	var results []NodeSystemInfo
	for rows.Next() {
		var info NodeSystemInfo
		// Note: The order of Scan arguments must exactly match the order of columns in the SELECT statement
		err := rows.Scan(
			&info.IP,
			&info.CPUCores,
			&info.CPUUsage,
			&info.Timestamp, // MySQL DATETIME/TIMESTAMP can be directly scanned into time.Time
		)
		if err != nil {
			// Consider whether to log the error and continue, or return the error immediately.
			// Here, returning the error immediately is chosen.
			return nil, fmt.Errorf("error scanning row for region '%s': %w", region, err)
		}
		results = append(results, info)
	}

	// Check for errors that occurred during rows iteration
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows for region '%s': %w", region, err)
	}

	if len(results) == 0 {
		// Optionally, you can return a specific error type or (nil, nil) to indicate no records found.
		// fmt.Printf("No records found for region: %s\n", region) // Example log message
	}
	return results, nil
}

// GetRedistributionProportion method queries the redistribution proportion from the first record in domain_config table.
func GetRedistributionProportion(db *sql.DB) (float64, error) {
	var redistributionProportion float64
	query := `
        SELECT
            redistribution_proportion
        FROM
            domain_config
        LIMIT 1;
    `
	err := db.QueryRow(query).Scan(&redistributionProportion)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0.0, fmt.Errorf("no records found in domain_config table: %w", err)
		}
		return 0.0, fmt.Errorf("error querying domain_config table: %w", err)
	}
	// Successfully fetched the value
	return redistributionProportion, nil
}

// GetTotalReqIncrementByRegion calculates and returns the total request increment
// for a specified region. It returns 0 if no increments are configured for that region.
func GetTotalReqIncrementByRegion(db *sql.DB, region string) (int, error) {
	// The SQL query now uses the SUM() aggregate function to calculate the total directly.
	// This is much more efficient than fetching all rows and accumulating the sum in the Go application.
	query := `
        SELECT
            SUM(req_increment)
        FROM
            domain_region_increment
        WHERE
            region = ?;
    `

	// Use QueryRow because we expect at most one row in the result set (i.e., the total sum).
	row := db.QueryRow(query, region)

	// IMPORTANT: When SUM() operates on an empty set (i.e., no records match the region),
	// SQL returns NULL, not 0. We must use a type like sql.NullInt64, which can handle
	// NULL values, to scan the result.
	var totalIncrement sql.NullInt64

	// Scan the query result into the totalIncrement variable.
	err := row.Scan(&totalIncrement)
	if err != nil {
		// If scanning fails, return an error. Note that sql.ErrNoRows is not expected here for a SUM()
		// query, as it returns a row with NULL for an empty set. This error check handles
		// other potential scanning or database issues.
		return 0, fmt.Errorf("error scanning total increment for region '%s': %w", region, err)
	}

	// Check the Valid field of the NullInt64.
	// If it's true, the database returned a non-NULL value.
	if totalIncrement.Valid {
		// Return the actual value, converting it from int64 to int.
		return int(totalIncrement.Int64), nil
	}

	// If totalIncrement.Valid is false, the database returned NULL.
	// This indicates that no records were found for the given region, so we treat the total as 0.
	return 0, nil
}

func GetClientDelay(db *sql.DB, sourceIP, targetIP string) (float64, error) {
	query := `
        SELECT delay_ms 
        FROM client_delay_info 
        WHERE source_ip = ? AND target_ip = ? 
          AND probe_time >= DATE_SUB(NOW(), INTERVAL 1 MINUTE)
        ORDER BY probe_time DESC 
        LIMIT 1
    `

	var delayMs int64
	err := db.QueryRow(query, sourceIP, targetIP).Scan(&delayMs)
	if err == nil {
		return float64(delayMs), nil
	}
	return 0, fmt.Errorf("query error: %w", err)
}

// GetLatestSourceIPByRegion finds the most recent source_ip associated with a given source_region.
func GetLatestSourceIPByRegion(db *sql.DB, sourceRegion string) (string, error) {
	var sourceIP string
	query := `
        SELECT
            source_ip
        FROM
            client_delay_info
        WHERE
            source_region = ?
        ORDER BY
            probe_time DESC
        LIMIT 1;
    `

	// db.QueryRow is suitable here because we expect at most one row.
	err := db.QueryRow(query, sourceRegion).Scan(&sourceIP)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("no source IP found for region '%s' in client_delay_info", sourceRegion)
		}
		return "", fmt.Errorf("database error when querying for source IP in region '%s': %w", sourceRegion, err)
	}

	return sourceIP, nil
}
func GetLatestConnectionCountByIP(db *sql.DB, ip string) (int, error) {
	var hostname string

	query := `
        SELECT
            hostname
        FROM
            system_info
        WHERE
            ip = ?
        ORDER BY
            timestamp DESC
        LIMIT 1;
    `

	err := db.QueryRow(query, ip).Scan(&hostname)

	if err != nil {
		if err == sql.ErrNoRows {
			// If no record is found for this IP, it's safe to assume 0 connections.
			// We return 0 and no error, as this is an expected state.
			return 0, nil
		}
		// For any other database error, return the error.
		return 0, fmt.Errorf("database error when querying for hostname for IP '%s': %w", ip, err)
	}

	// The hostname is stored as a string (VARCHAR), but we need an integer (connection count).
	// We must convert it.
	connectionCount, err := strconv.Atoi(hostname)
	if err != nil {
		// If the hostname is not a valid number (e.g., it's an actual name),
		// we should handle this case. Returning 0 is a safe default.
		// We also log a warning because this indicates a potential data inconsistency.
		log.Warnf("Could not parse hostname '%s' as an integer for IP '%s'. Defaulting to 0 connections. Error: %v", hostname, ip, err)
		return 0, nil
	}

	return connectionCount, nil
}
