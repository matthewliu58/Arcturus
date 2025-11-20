package probing_client

import (
	"context"
	"forwarding/metrics_processing/protocol"
	"forwarding/metrics_processing/storage"

	"github.com/sirupsen/logrus"
)

// ConfigServiceImpl is the implementation of the protocol.ConfigServiceServer interface.
type ConfigServiceImpl struct {
	protocol.UnimplementedConfigServiceServer
	fileManager *storage.FileManager
}

// NewConfigServiceImpl creates a new ConfigServiceImpl instance.
func NewConfigServiceImpl(fm *storage.FileManager) *ConfigServiceImpl {
	return &ConfigServiceImpl{
		fileManager: fm,
	}
}

// PushConfig implements the logic for receiving and processing configuration pushes from the control plane.
func (s *ConfigServiceImpl) PushConfig(ctx context.Context, req *protocol.PushConfigRequest) (*protocol.SimpleResponse, error) {
	logrus.Infof("Received PushConfig request from control plane")

	// Save NodeList
	if req.NodeList != nil {
		err := s.fileManager.SaveNodeList(req.NodeList)
		if err != nil {
			logrus.Errorf("Failed to save node list: %v", err)
			return &protocol.SimpleResponse{Status: "ERROR", Message: "Failed to save node list"}, err
		}
		logrus.Infof("Successfully saved updated node list.")
	}

	// Save ProbeTasks
	if req.ProbeTasks != nil {
		err := s.fileManager.SaveProbeTasks(req.ProbeTasks)
		if err != nil {
			logrus.Errorf("Failed to save probing_report metrics_tasks: %v", err)
			return &protocol.SimpleResponse{Status: "ERROR", Message: "Failed to save probing_report metrics_tasks"}, err
		}
		logrus.Infof("Successfully saved updated probing_report metrics_tasks.")
	}

	// Save DomainIpMappings
	if req.DomainIpMappings != nil {
		err := s.fileManager.SaveDomainIPMappings(req.DomainIpMappings)
		if err != nil {
			logrus.Errorf("Failed to save domain ip mappings: %v", err)
			return &protocol.SimpleResponse{Status: "ERROR", Message: "Failed to save domain ip mappings"}, err
		}
		logrus.Infof("Successfully saved updated domain ip mappings.")
	}

	return &protocol.SimpleResponse{Status: "OK", Message: "Configuration pushed successfully"}, nil
}
