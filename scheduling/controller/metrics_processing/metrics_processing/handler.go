// metrics_processing/handler.go
package metrics_processing

import (
	"context"
	"database/sql"
	"fmt"
	"scheduling/controller/metrics_processing/metrics_evaluating"
	"scheduling/controller/metrics_processing/metrics_storage"
	"scheduling/controller/metrics_processing/metrics_tasks"
	pb "scheduling/controller/metrics_processing/protocol"
	"scheduling/controller/metrics_processing/task_dispatching"
	"scheduling/db_models"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
)

type Handler struct {
	pb.UnimplementedMetricsServiceServer
	db               *sql.DB
	fileManager      *metrics_storage.FileManager
	taskGenerator    *metrics_tasks.TaskGenerator
	configPusher     *task_dispatching.Pusher
	assessmentCalc   *metrics_evaluating.Calculator
	processor        *Processor
	generatorStarted atomic.Bool
	calcStarted      atomic.Bool
	etcdSync         *EtcdSync
	appCtx           context.Context // Add application context for graceful shutdown
}

func NewHandler(
	db *sql.DB,
	fileManager *metrics_storage.FileManager,
	taskGenerator *metrics_tasks.TaskGenerator,
	configPusher *task_dispatching.Pusher,
	etcdSync *EtcdSync, // etcd
	assessmentCalc *metrics_evaluating.Calculator,
) *Handler {
	handler := &Handler{
		db:             db,
		fileManager:    fileManager,
		taskGenerator:  taskGenerator,
		configPusher:   configPusher,
		etcdSync:       etcdSync,
		assessmentCalc: assessmentCalc,
		processor:      NewProcessor(db),
		appCtx:         context.Background(), // Initialize with a default non-cancellable context
	}

	handler.generatorStarted.Store(false)
	handler.calcStarted.Store(false)

	return handler
}

// SetAppContext allows setting the main application's cancellable context.
func (h *Handler) SetAppContext(ctx context.Context) {
	h.appCtx = ctx
}

func (h *Handler) InitDataPlane(ctx context.Context, req *pb.InitRequest) (*pb.SimpleResponse, error) {
	nodeIP := req.Metrics.Ip
	log.Infof("InitDataPlane, nodeIP=%s", nodeIP)

	err := db_models.InsertMetricsInfo(h.db, req.Metrics)
	if err != nil {
		return &pb.SimpleResponse{
			Status:  "error",
			Message: fmt.Sprintf(": %v", err),
		}, nil
	}

	// Ensure background services are running.
	// This will start them on the very first InitDataPlane call.
	if !h.generatorStarted.Load() {
		if h.generatorStarted.CompareAndSwap(false, true) {
			log.Infof("First node registered. Starting TaskGenerator background process.")
			go h.taskGenerator.StartTaskGenerator(h.appCtx) // Use the cancellable app context
		}
	}
	if !h.calcStarted.Load() {
		if h.calcStarted.CompareAndSwap(false, true) {
			log.Infof("First node registered. Starting AssessmentCalculator background process.")
			go h.assessmentCalc.StartAssessmentCalculator(h.appCtx) // Use the cancellable app context
		}
	}

	// For every new node, immediately regenerate metrics_tasks and push to all nodes.
	log.Infof("New node %s registered. Triggering immediate task regeneration and push.", nodeIP)
	if h.taskGenerator.GenerateTasksOnDemand() {
		log.Infof("Task regeneration triggered by new node.")

		nodeList := h.fileManager.GetNodeList()
		if nodeList != nil && len(nodeList.Nodes) > 0 {
			h.configPusher.PushToAllNodes(nodeList, h.fileManager)
			log.Infof("Pushed updated metrics_tasks to all %d nodes.", len(nodeList.Nodes))
		} else {
			log.Warningf("Cannot push metrics_tasks, node list is empty.")
		}
	} else {
		log.Infof("Task regeneration was not needed at this time.")
	}

	return &pb.SimpleResponse{
		Status:  "ok",
		Message: fmt.Sprintf(""),
	}, nil
}

/*func (h *Handler) handleBufferPeriodEnd() {
	log.Infof("handleBufferPeriodEnd, bufferPeriod=%v", h.bufferPeriod)

	if h.taskGenerator.GenerateTasksOnDemand() {
		log.Infof("GenerateTasksIfNeeded")

		nodeList := h.fileManager.GetNodeList()
		if nodeList != nil && len(nodeList.Nodes) > 0 {

			h.configPusher.PushToAllNodes(nodeList, h.fileManager)
			log.Infof("PushToAllNodes, %d", len(nodeList.Nodes))
		} else {
			log.Warningf("nodeList is nil")
		}
	} else {
		log.Infof("do not need GenerateTasksIfNeeded")
	}

	if !h.calcStarted.Load() {
		if h.calcStarted.CompareAndSwap(false, true) {
			log.Infof("calcStarted")
			go h.assessmentCalc.StartAssessmentCalculator(context.Background())
		}
	}
}*/

func (h *Handler) SyncMetrics(ctx context.Context, req *pb.SyncRequest) (*pb.SyncResponse, error) {

	err := db_models.InsertMetricsInfo(h.db, req.Metrics)
	if err != nil {
		return &pb.SyncResponse{
			Status:  "error",
			Message: fmt.Sprintf(": %v", err),
		}, nil
	}

	nodeListNeedsUpdate := false
	probeTasksNeedUpdate := false
	domainIPMappingsNeedUpdate := false

	if req.NodeListHash != h.fileManager.GetNodeListHash() {
		nodeListNeedsUpdate = true
	}

	if req.ProbeTasksHash != h.fileManager.GetNodeTasksHash(req.Metrics.Ip) {
		probeTasksNeedUpdate = true
	}

	if req.DomainIpMappingsHash != h.fileManager.GetDomainIPMappingsHash() {
		domainIPMappingsNeedUpdate = true
	}

	resp := &pb.SyncResponse{
		Status:                     "ok",
		Message:                    "",
		NeedUpdateNodeList:         nodeListNeedsUpdate,
		NeedUpdateProbeTasks:       probeTasksNeedUpdate,
		NeedUpdateDomainIpMappings: domainIPMappingsNeedUpdate,
	}

	if nodeListNeedsUpdate {
		resp.NodeList = h.fileManager.GetNodeList()
	}

	if probeTasksNeedUpdate {
		resp.ProbeTasks = h.fileManager.GetNodeTasks(req.Metrics.Ip)
	}

	if domainIPMappingsNeedUpdate {
		resp.DomainIpMappings = h.fileManager.GetDomainIPMappings()
	}

	if len(req.RegionProbeResults) > 0 {
		// err := h.processor.ProcessProbeResults(req.Metrics.Ip, req.RegionProbeResults, h.fileManager)
		err := h.processor.ProcessProbeResults(req.Metrics.Ip, req.RegionProbeResults)
		if err != nil {
			log.Errorf("ProcessProbeResults failed, err=%v", err)
		}
	}
	regionAssessments := h.assessmentCalc.GetCachedAssessments()
	log.Infof(" req.Metrics.Ip:%s  regionAssessments:%d ", req.Metrics.Ip, len(regionAssessments))
	if len(regionAssessments) > 0 {
		resp.RegionAssessments = regionAssessments
		log.Infof(" req.Metrics.Ip:%s  regionAssessments:%d ", req.Metrics.Ip, len(regionAssessments))
	}
	return resp, nil
}

/*
func (h *Handler) StartBackgroundServicesWhenReady(ctx context.Context) {
	if h.generatorStarted.Load() && h.calcStarted.Load() {
		log.Infof("already StartBackgroundServicesWhenReady")
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	log.Infof("StartBackgroundServicesWhenReady")
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if h.generatorStarted.Load() && h.calcStarted.Load() {
				log.Infof("already StartBackgroundServicesWhenReady")
				return
			}
			nodesCount, err := db_models.CountMetricsNodes(h.db)
			if err != nil {
				log.Errorf("CountMetricsNodes failed, err: %v", err)
				continue
			}
			// receive dataPlane node info
			if nodesCount > 0 {
				h.startBackgroundServicesIfNeeded(ctx)
				if h.generatorStarted.Load() && h.calcStarted.Load() {
					return
				}
			}
			log.Infof("StartBackgroundServicesWhenReady, nodesCount: %v", nodesCount)
		}
	}
}

func (h *Handler) startBackgroundServicesIfNeeded(ctx context.Context) {
	if !h.generatorStarted.Load() {
		if h.generatorStarted.CompareAndSwap(false, true) {
			log.Infof("generatorStarted")
			go h.taskGenerator.StartTaskGenerator(ctx)
		}
	}
	if !h.calcStarted.Load() {
		if h.calcStarted.CompareAndSwap(false, true) {
			log.Infof("calcStarted")
			go h.assessmentCalc.StartAssessmentCalculator(ctx)
		}
	}
}
*/
