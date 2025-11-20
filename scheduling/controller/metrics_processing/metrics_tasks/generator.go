package metrics_tasks

import (
	"context"
	"database/sql"
	log "github.com/sirupsen/logrus"
	"scheduling/controller/metrics_processing/metrics_storage"
	pb "scheduling/controller/metrics_processing/protocol"
	"scheduling/db_models"
	"sync"
	"time"
)

// TaskGenerator
type TaskGenerator struct {
	db          *sql.DB
	fileManager *metrics_storage.FileManager
	mutex       sync.Mutex
	lastGenTime time.Time
	interval    time.Duration //
}

// NewTaskGenerator
func NewTaskGenerator(db *sql.DB, fileManager *metrics_storage.FileManager, interval time.Duration) *TaskGenerator {
	return &TaskGenerator{
		db:          db,
		fileManager: fileManager,
		interval:    interval,
	}
}

// generateTasks is the internal implementation for task generation.
// It is NOT thread-safe and must be called within a lock.
func (tg *TaskGenerator) generateTasks() bool {
	// Update the generation time
	tg.lastGenTime = time.Now()

	// Query all node information from the database
	nodeInfos, err := db_models.QueryNodeInfo(tg.db)
	if err != nil {
		log.Errorf("QueryNodeInfo failed, err=%v", err)
		return false
	}

	// Save the complete list of nodes to a file
	nodeList := &pb.NodeList{
		Nodes: nodeInfos,
	}
	if err := tg.fileManager.SaveNodeList(nodeList); err != nil {
		log.Printf("failed to save node list: %v", err)
	}

	// Query and save domain-IP mappings
	domainIPMappings, err := db_models.QueryDomainIPMappings(tg.db)
	if err != nil {
		log.Errorf("QueryDomainIPMappings failed, err=%v", err)
	} else {
		if err := tg.fileManager.SaveDomainIPMappings(domainIPMappings); err != nil {
			log.Errorf("SaveDomainIPMappings failed, err=%v", err)
		}
	}

	// Generate all-to-all probing_report metrics_tasks
	for _, sourceNode := range nodeInfos {
		sourceIP := sourceNode.Ip
		var tasks []*pb.ProbeTask
		for _, targetNode := range nodeInfos {
			targetIP := targetNode.Ip
			if sourceIP == targetIP {
				continue // Do not probing_report self
			}
			task := &pb.ProbeTask{
				TaskId:   generateTaskID(sourceIP, targetIP),
				TargetIp: targetIP,
			}
			tasks = append(tasks, task)
		}

		// Save the generated metrics_tasks for the source node
		if err := tg.fileManager.SaveNodeTasks(sourceIP, tasks); err != nil {
			log.Errorf("SaveNodeTasks failed, sourceIP=%s : err=%v", sourceIP, err)
		}
	}

	log.Infof("Successfully generated and saved probing_report metrics_tasks for %d nodes.", len(nodeInfos))
	return true
}

// GenerateTasksPeriodically checks if the interval has passed and generates metrics_tasks if needed.
// This is intended for the background ticker loop.
func (tg *TaskGenerator) GenerateTasksPeriodically() bool {
	tg.mutex.Lock()
	defer tg.mutex.Unlock()

	// Check if the generation interval has passed
	if time.Since(tg.lastGenTime) < tg.interval {
		return false // Not time yet
	}

	return tg.generateTasks()
}

// GenerateTasksOnDemand generates metrics_tasks immediately, bypassing the time interval check.
// This is intended for on-demand execution, e.g., when a new node registers.
func (tg *TaskGenerator) GenerateTasksOnDemand() bool {
	tg.mutex.Lock()
	defer tg.mutex.Unlock()

	log.Infof("Forcing task generation on-demand.")
	return tg.generateTasks()
}

// ID
func generateTaskID(sourceIP, targetIP string) string {
	return "task_" + sourceIP + "_" + targetIP
}

// StartTaskGenerator
func (tg *TaskGenerator) StartTaskGenerator(ctx context.Context) {

	ticker := time.NewTicker(tg.interval / 2) //
	defer ticker.Stop()

	// This initial call is no longer needed here because InitDataPlane
	// now triggers the first task generation when the first node registers.
	// tg.GenerateTasksIfNeeded()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if tg.GenerateTasksPeriodically() {
				log.Infof("Tasks generated periodically.")
			}
		}
	}
}
