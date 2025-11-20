package metrics_processing

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"scheduling/controller/metrics_processing/metrics_storage"
	pb "scheduling/controller/metrics_processing/protocol"
	"scheduling/db_models"
	"scheduling/goroutine_pool"
	"strings"
	"sync"

	clientv3 "go.etcd.io/etcd/client/v3"

	"go.etcd.io/etcd/api/v3/mvccpb"
	"google.golang.org/protobuf/proto"
)

const EtcdSyncPool = "etcd_sync_pool"

type UpdateData struct {
	Region string
	Key    string
	Value  []byte
	Sync   *EtcdSync
}

type EtcdSync struct {
	client          *clientv3.Client
	controllerID    string
	db              *sql.DB
	processor       *Processor
	fileManager     *metrics_storage.FileManager
	watchCancels    map[string]context.CancelFunc
	watchMutex      sync.Mutex
	dataAccessMutex sync.RWMutex
}

func processUpdateTask(data interface{}) {
	if updateData, ok := data.(*UpdateData); ok {
		updateData.Sync.handleDataUpdate(updateData.Region, updateData.Key, updateData.Value)
	} else {
		log.Printf(": ")
	}
}

func InitPoolFromConfig(poolNum int) {

	goroutine_pool.InitPool(EtcdSyncPool, poolNum, processUpdateTask)
	log.Printf("etcd，: %d", poolNum)
}

func NewEtcdSync(
	db *sql.DB,
	processor *Processor,
	fileManager *metrics_storage.FileManager,
	controllerID string,
	etcdEndpoints []string,
) (*EtcdSync, error) {

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   etcdEndpoints,
		DialTimeout: 5 * 1000000000,
	})

	if err != nil {
		return nil, fmt.Errorf("etcd: %v", err)
	}

	s := &EtcdSync{
		client:       client,
		controllerID: controllerID,
		db:           db,
		processor:    processor,
		fileManager:  fileManager,
		watchCancels: make(map[string]context.CancelFunc),
	}

	if goroutine_pool.GetPool(EtcdSyncPool) == nil {
		InitPoolFromConfig(50)
	}

	if err := s.setupRegionWatchers(); err != nil {
		client.Close()
		return nil, err
	}

	log.Printf("etcd，ID: %s", controllerID)
	return s, nil
}

func (s *EtcdSync) Close() {

	s.watchMutex.Lock()
	for _, cancel := range s.watchCancels {
		cancel()
	}
	s.watchCancels = make(map[string]context.CancelFunc)
	s.watchMutex.Unlock()

	s.client.Close()
	log.Println("etcd")
}

func (s *EtcdSync) SyncMetricsToEtcd(metrics *pb.Metrics) error {

	region, err := db_models.GetNodeRegion(s.db, metrics.Ip)
	if err != nil {
		log.Printf(": : %v，unknown", err)
		region = "unknown"
	}

	key := fmt.Sprintf("data/regions/%s/%s/metrics_processing", region, metrics.Ip)

	data, err := proto.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("Metrics: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*1000000000) // 5
	defer cancel()

	_, err = s.client.Put(ctx, key, string(data))
	if err != nil {
		return fmt.Errorf("Metricsetcd: %v", err)
	}

	log.Printf(" %s Metricsetcd，: %s", metrics.Ip, region)
	return nil
}

func (s *EtcdSync) SyncProbeResults(nodeIP string, results []*pb.RegionProbeResult) error {

	region, err := db_models.GetNodeRegion(s.db, nodeIP)
	if err != nil {
		log.Printf(": : %v，unknown", err)
		region = "unknown"
	}

	for _, probeResult := range results {

		key := fmt.Sprintf("data/regions/%s/%s/probe_results/%s",
			region, nodeIP, probeResult.Region)

		data, err := proto.Marshal(probeResult)
		if err != nil {
			log.Printf(": %v", err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*1000000000)
		_, err = s.client.Put(ctx, key, string(data))
		cancel()

		if err != nil {
			log.Printf("etcd: %v", err)
			continue
		}
	}

	log.Printf(" %s etcd，: %s", nodeIP, region)
	return nil
}

func (s *EtcdSync) setupRegionWatchers() error {

	regions, err := db_models.GetAllRegions(s.db)
	if err != nil {
		log.Printf(": %v，default", err)
		regions = []string{"default"}
	}

	for _, region := range regions {
		if err := s.watchRegion(region); err != nil {
			return err
		}
	}

	log.Printf(" %d etcd", len(regions))
	return nil
}

func (s *EtcdSync) watchRegion(region string) error {

	prefix := fmt.Sprintf("data/regions/%s/", region)

	ctx, cancel := context.WithCancel(context.Background())

	s.watchMutex.Lock()
	s.watchCancels[region] = cancel
	s.watchMutex.Unlock()

	watchChan := s.client.Watch(ctx, prefix, clientv3.WithPrefix())

	go func() {
		for watchResp := range watchChan {
			for _, event := range watchResp.Events {
				if event.Type == mvccpb.PUT {

					updateData := &UpdateData{
						Region: region,
						Key:    string(event.Kv.Key),
						Value:  event.Kv.Value,
						Sync:   s,
					}

					etcdPool := goroutine_pool.GetPool(EtcdSyncPool)
					if etcdPool != nil {

						if err := etcdPool.Invoke(updateData); err != nil {
							log.Printf(": %v", err)

							s.handleDataUpdate(region, string(event.Kv.Key), event.Kv.Value)
						}
					} else {

						log.Printf("etcd，")
						s.handleDataUpdate(region, string(event.Kv.Key), event.Kv.Value)
					}
				}
			}
		}
		log.Printf(" %s ", region)
	}()

	log.Printf(" %s ", region)
	return nil
}

func (s *EtcdSync) handleDataUpdate(region, key string, value []byte) {

	parts := strings.Split(key, "/")
	if len(parts) < 4 {
		log.Printf(": %s", key)
		return
	}

	nodeIP := parts[3]

	if strings.Contains(key, "/metrics_processing") {

		s.handleMetricsUpdate(region, nodeIP, value)
	} else if strings.Contains(key, "/probe_results/") {

		targetRegion := parts[5]
		s.handleProbeResultsUpdate(region, nodeIP, targetRegion, value)
	}
}

func (s *EtcdSync) handleMetricsUpdate(region, nodeIP string, value []byte) {

	metrics := &pb.Metrics{}
	if err := proto.Unmarshal(value, metrics); err != nil {
		log.Printf("metrics_processing: %v", err)
		return
	}

	s.dataAccessMutex.Lock()
	defer s.dataAccessMutex.Unlock()

	err := db_models.InsertMetricsInfo(s.db, metrics)
	if err != nil {
		log.Printf("metrics_processing: %v", err)
		return
	}

	log.Printf(" %s metrics_processing，: %s", nodeIP, region)
}

func (s *EtcdSync) handleProbeResultsUpdate(sourceRegion, nodeIP, targetRegion string, value []byte) {

	probeResult := &pb.RegionProbeResult{}
	if err := proto.Unmarshal(value, probeResult); err != nil {
		log.Printf(": %v", err)
		return
	}

	s.dataAccessMutex.Lock()
	defer s.dataAccessMutex.Unlock()

	//err := s.processor.ProcessProbeResults(nodeIP, []*pb.RegionProbeResult{probeResult}, s.fileManager)
	err := s.processor.ProcessProbeResults(nodeIP, []*pb.RegionProbeResult{probeResult})
	if err != nil {
		log.Printf(": %v", err)
		return
	}

	log.Printf(" %s ，: %s，: %s",
		nodeIP, sourceRegion, targetRegion)
}

func (s *EtcdSync) AddRegionWatcher(region string) error {

	s.watchMutex.Lock()
	if _, exists := s.watchCancels[region]; exists {
		s.watchMutex.Unlock()
		log.Printf(" %s ", region)
		return nil
	}
	s.watchMutex.Unlock()

	return s.watchRegion(region)
}

func (s *EtcdSync) RemoveRegionWatcher(region string) {
	s.watchMutex.Lock()
	if cancel, exists := s.watchCancels[region]; exists {

		cancel()
		delete(s.watchCancels, region)
		log.Printf(" %s ", region)
	}
	s.watchMutex.Unlock()
}
