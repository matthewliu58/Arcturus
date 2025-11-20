package metrics_processing

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"scheduling/controller/metrics_processing/metrics_evaluating"
	"scheduling/controller/metrics_processing/metrics_processing"
	"scheduling/controller/metrics_processing/metrics_storage"
	"scheduling/controller/metrics_processing/metrics_tasks"
	pb "scheduling/controller/metrics_processing/protocol"
	cf "scheduling/controller/metrics_processing/task_dispatching"
	"scheduling/controller/metrics_processing/utils"
	"time"

	log "github.com/sirupsen/logrus"

	"google.golang.org/grpc"
)

type ServerConfig struct {
	ListenAddr string
	DataDir    string
}

type HeartbeatServer struct {
	config          ServerConfig
	db              *sql.DB
	grpcServer      *grpc.Server
	fileManager     *metrics_storage.FileManager
	metricsHandler  *metrics_processing.Handler
	shutdownHandler utils.ShutdownHandler
}

func NewHeartbeatServer(ctx context.Context, config ServerConfig, db *sql.DB) (*HeartbeatServer, error) {

	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf(": %v", err)
	}

	fileManager, err := metrics_storage.NewFileManager(config.DataDir)
	if err != nil {
		return nil, fmt.Errorf(": %v", err)
	}

	configPusher := cf.NewPusher()

	taskGenerator := metrics_tasks.NewTaskGenerator(db, fileManager, 5*time.Minute)

	assessmentCalc := metrics_evaluating.NewAssessmentCalculator(db, 1*time.Minute)

	// Start the background job for calculating RegionAssessments
	go assessmentCalc.StartAssessmentCalculator(ctx)
	log.Infof("AssessmentCalculator background job started")

	metricsHandler := metrics_processing.NewHandler(
		db,
		fileManager,
		taskGenerator,
		configPusher,
		nil,
		assessmentCalc,
	)
	// Pass the application's cancellable context to the handler.
	metricsHandler.SetAppContext(ctx)

	return &HeartbeatServer{
		config:         config,
		db:             db,
		fileManager:    fileManager,
		metricsHandler: metricsHandler,

		shutdownHandler: utils.NewShutdownHandler(func() {
			configPusher.Release()
			utils.ReleasePoolResources()
			//middleware.CloseDB()
			log.Infof("server down ")
		}),
	}, nil
}

func (s *HeartbeatServer) Start(ctx context.Context) error {
	s.grpcServer = grpc.NewServer()
	pb.RegisterMetricsServiceServer(s.grpcServer, s.metricsHandler)

	lis, err := net.Listen("tcp", s.config.ListenAddr)
	if err != nil {
		return fmt.Errorf(": %v", err)
	}

	// Graceful shutdown logic for the gRPC server.
	go func() {
		<-ctx.Done() // Wait for the cancellation signal from main.
		log.Infof("Metrics gRPC server is shutting down...")
		s.Stop()
	}()

	log.Infof("HeartbeatServer Start, ListenAddr=%s", s.config.ListenAddr)
	return s.grpcServer.Serve(lis)
}

func (s *HeartbeatServer) Stop() {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
	s.shutdownHandler.ExecuteShutdown()
}

func StartServer(ctx context.Context, db *sql.DB) {

	/*db := middleware.ConnectToDB()
	if db == nil {
		log.Fatal("")
	}*/

	dataDir := "../../sql/"

	addr := "0.0.0.0:8080"

	config := ServerConfig{
		ListenAddr: addr,
		DataDir:    dataDir,
	}

	server, err := NewHeartbeatServer(ctx, config, db)
	if err != nil {
		log.Fatalf("NewHeartbeatServer failed, err=%v", err)
	}

	if err := server.Start(ctx); err != nil {
		log.Fatalf("Start failed, err=%v", err)
	}
}
