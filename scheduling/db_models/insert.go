package db_models

import (
	"database/sql"
	"fmt"
	log "github.com/sirupsen/logrus"
	pb "scheduling/controller/metrics_processing/protocol"
	"scheduling/structs"
	"time"
)

type ProbeResult struct {
	SourceIP     string
	SourceRegion string
	TargetIP     string
	TargetRegion string
	TCPDelay     int64
	ProbeTime    time.Time
}

func InsertMetricsInfo(db *sql.DB, info *pb.Metrics) error {
	query := `
		INSERT INTO system_info (
			ip, 
			cpu_cores, cpu_model_name, cpu_mhz, cpu_cache_size, cpu_usage,
			memory_total, memory_available, memory_used, memory_used_percent,
			disk_device, disk_total, disk_free, disk_used, disk_used_percent,
			network_interface_name, network_bytes_sent, network_bytes_recv,
			network_packets_sent, network_packets_recv,
			hostname, os, platform, platform_version, uptime,
			load1, load5, load15, timestamp
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("Timestamp: %s\n", timestamp)
	_, err := db.Exec(query,
		info.Ip,
		info.CpuInfo.Cores, info.CpuInfo.ModelName, info.CpuInfo.Mhz, info.CpuInfo.CacheSize, info.CpuInfo.Usage, //5
		info.MemoryInfo.Total, info.MemoryInfo.Available, info.MemoryInfo.Used, info.MemoryInfo.UsedPercent, //4
		info.DiskInfo.Device, info.DiskInfo.Total, info.DiskInfo.Free, info.DiskInfo.Used, info.DiskInfo.UsedPercent, //5
		info.NetworkInfo.InterfaceName, info.NetworkInfo.BytesSent, info.NetworkInfo.BytesRecv, //3
		info.NetworkInfo.PacketsSent, info.NetworkInfo.PacketsRecv, //2
		info.HostInfo.Hostname, info.HostInfo.Os, info.HostInfo.Platform, info.HostInfo.PlatformVersion, info.HostInfo.Uptime, //5
		info.LoadInfo.Load1, info.LoadInfo.Load5, info.LoadInfo.Load15, timestamp, //4
	)
	return err
}

func InsertLinkInfo(db *sql.DB, sourceIP string, destinationIP string, delay float64, timestamp string) error {
	query := `
		INSERT INTO link_info (source_ip, destination_ip, latency, Timestamp)
		VALUES (?, ?, ?, ?)
	`
	_, err := db.Exec(query, sourceIP, destinationIP, delay, timestamp)
	return err
}

func UpdateVirtualQueueAndCPUMetrics(db *sql.DB, sourceIP, destinationIP string, latency, mean, variance, VirtualQueueCPUMean, VirtualQueueCPUVariance float64) error {
	query := `
		INSERT INTO network_metrics (
			source_ip, destination_ip, link_latency, 
			cpu_mean, cpu_variance, 
			virtual_queue_cpu_mean, virtual_queue_cpu_variance
		) VALUES (?, ?, ?, ?, ?, ?, ?);
	`

	_, err := db.Exec(query, sourceIP, destinationIP, latency,
		mean, variance, VirtualQueueCPUMean, VirtualQueueCPUVariance)
	if err != nil {
		return fmt.Errorf("failed to insert link data: %v", err)
	}

	return nil
}

func InsertProbeResult(db *sql.DB, result *ProbeResult) error {
	query := `
    INSERT INTO region_probe_info 
    (source_ip, source_region, target_ip, target_region, tcp_delay, probe_time) 
    VALUES (?, ?, ?, ?, ?, ?)
    `
	_, err := db.Exec(
		query,
		result.SourceIP,
		result.SourceRegion,
		result.TargetIP,
		result.TargetRegion,
		result.TCPDelay,
		result.ProbeTime,
	)
	return err
}

// InsertDomainOrigins inserts data into the domain_origin table
func InsertDomainOrigins(db *sql.DB, domains []structs.DomainOriginEntry) error {
	if len(domains) == 0 {
		log.Infof("No domain origins to insert.")
		return nil
	}

	// Prepare statement for inserting data
	// Using ON DUPLICATE KEY UPDATE to handle cases where the domain might already exist.
	// You can choose to error out instead if that's preferred.
	stmt, err := db.Prepare("INSERT INTO domain_origin (domain, origin_ip) VALUES (?, ?) ON DUPLICATE KEY UPDATE origin_ip = VALUES(origin_ip)")
	if err != nil {
		return fmt.Errorf("error preparing domain_origin insert statement: %w", err)
	}
	defer stmt.Close()

	for _, d := range domains {
		_, err := stmt.Exec(d.Domain, d.OriginIP)
		if err != nil {
			// Log individual errors but continue if possible, or return immediately
			log.Errorf("Error inserting domain_origin (domain: %s, ip: %s): %v", d.Domain, d.OriginIP, err)
			// return fmt.Errorf("error executing domain_origin insert for domain %s: %w", d.Domain, err) // Uncomment to stop on first error
		} else {
			log.Infof("Successfully inserted/updated domain_origin: %s -> %s", d.Domain, d.OriginIP)
		}
	}
	return nil // Return nil if we are logging errors but continuing
}

// InsertNodeRegions inserts data into the node_region table
func InsertNodeRegions(db *sql.DB, nodes []structs.NodeRegionEntry) error {
	if len(nodes) == 0 {
		log.Infof("No node regions to insert.")
		return nil
	}

	// Prepare statement for inserting data
	// Using ON DUPLICATE KEY UPDATE for the unique IP.
	// Note: 'id' is auto-increment, 'created_at' has a default.
	stmt, err := db.Prepare(`
		INSERT INTO node_region (ip, region, hostname, description) 
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE 
			region = VALUES(region), 
			hostname = VALUES(hostname), 
			description = VALUES(description)
	`)
	if err != nil {
		return fmt.Errorf("error preparing node_region insert statement: %w", err)
	}
	defer stmt.Close()

	for _, n := range nodes {
		// Handle potentially NULL/empty optional fields from TOML
		var hostname sql.NullString
		if n.Hostname != "" {
			hostname.String = n.Hostname
			hostname.Valid = true
		}

		var description sql.NullString
		if n.Description != "" {
			description.String = n.Description
			description.Valid = true
		}

		_, err := stmt.Exec(n.IP, n.Region, hostname, description)
		if err != nil {
			log.Errorf("Error inserting node_region (ip: %s, region: %s): %v", n.IP, n.Region, err)
			// return fmt.Errorf("error executing node_region insert for IP %s: %w", n.IP, err) // Uncomment to stop on first error
		} else {
			log.Infof("Successfully inserted/updated node_region: %s (%s)", n.IP, n.Region)
		}
	}
	return nil
}

func SaveOrUpdateDomainConfig(db *sql.DB, domainName string, totalReqIncrement int, regionReqIncrement map[string]int, redistributionProportion float64) error {
	// Handle the domain_config table - store redistribution_proportion for each domain
	// Use INSERT ... ON DUPLICATE KEY UPDATE to either insert a new record or update an existing one
	_, err := db.Exec(`
		INSERT INTO domain_config (domain_name, redistribution_proportion) 
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE 
			redistribution_proportion = VALUES(redistribution_proportion),
			last_updated = CURRENT_TIMESTAMP
	`, domainName, redistributionProportion)
	if err != nil {
		return fmt.Errorf("failed to insert/update domain_config: %v", err)
	}

	// Then, handle the domain_region_increment table
	// Delete existing records for this domain
	_, err = db.Exec("DELETE FROM domain_region_increment WHERE domain_name = ?", domainName)
	if err != nil {
		return fmt.Errorf("failed to delete existing domain_region_increment records: %v", err)
	}

	// Insert new records for each region
	for region, reqIncrement := range regionReqIncrement {
		_, err = db.Exec("INSERT INTO domain_region_increment (domain_name, region, req_increment) VALUES (?, ?, ?)",
			domainName, region, reqIncrement)
		if err != nil {
			return fmt.Errorf("failed to insert into domain_region_increment: %v", err)
		}
	}

	return nil
}
