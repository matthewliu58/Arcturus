package client_probing_api

import (
	"database/sql"
	"fmt"
	log "github.com/sirupsen/logrus"
	"scheduling/db_models"
	"time"
)

// ClientDelayDAO handles database write operations for probing_client delay data.
type ClientDelayDAO struct {
	db *sql.DB
}

// NewClientDelayDAO creates a new instance of ClientDelayDAO.
func NewClientDelayDAO(db *sql.DB) *ClientDelayDAO {
	return &ClientDelayDAO{db: db}
}

// BatchInsertClientDelays inserts multiple probing_client delay records into the database.
// This implementation queries the region for each target IP inside the loop,
func (d *ClientDelayDAO) BatchInsertClientDelays(sourceIP, sourceRegion string, delays []DelayData) error {
	// Start a transaction. This ensures that all insertions in the batch
	// either succeed together or fail together.
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	// Defer a rollback. If the transaction is successfully committed, this will do nothing.
	// If any error occurs and the function returns, this will ensure the transaction is rolled back.
	defer tx.Rollback()

	// Prepare the SQL statement for insertion. Preparing it outside the loop is efficient.
	query := `
        INSERT INTO client_delay_info 
        (source_ip, source_region, target_ip, target_region, delay_ms, probe_time) 
        VALUES (?, ?, ?, ?, ?, ?)
    `
	stmt, err := tx.Prepare(query)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	// Use a single timestamp for the entire batch, representing when the report was processed.
	probeTime := time.Now()

	// Iterate through each delay record and insert it.
	for _, delay := range delays {
		// Query the region for the target IP individually.
		// Note: This database call is inside the loop.
		targetRegion, err := db_models.GetNodeRegion(d.db, delay.TargetIP)
		// We ignore the error and check if the region is empty, defaulting to "unknown".
		if err != nil || targetRegion == "" {
			targetRegion = "unknown"
		}

		// The database table 'client_delay_info' expects delay_ms as an INT.
		delayMsInt := int(delay.DelayMS)

		// Execute the prepared statement with the current delay's data.
		_, err = stmt.Exec(
			sourceIP,
			sourceRegion,
			delay.TargetIP,
			targetRegion,
			delayMsInt,
			probeTime,
		)
		if err != nil {
			// If any insertion fails, return the error. The deferred tx.Rollback() will execute.
			return fmt.Errorf("failed to execute insert for target %s: %w", delay.TargetIP, err)
		}
	}

	// If all insertions were successful, commit the transaction.
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.WithFields(log.Fields{
		"sourceIP":    sourceIP,
		"recordCount": len(delays),
	}).Info("Successfully committed probing_client delay batch insert.")

	return nil
}
