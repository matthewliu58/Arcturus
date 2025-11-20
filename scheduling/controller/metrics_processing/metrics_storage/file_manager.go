package metrics_storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	pb "scheduling/controller/metrics_processing/protocol"
)

type FileManager struct {
	dataDir              string
	nodeListFile         string
	taskMapFile          string
	domainIPMappingsFile string
	cacheManager         *CacheManager
}

func NewFileManager(dataDir string) (*FileManager, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf(": %v", err)
	}

	fm := &FileManager{
		dataDir:              dataDir,
		nodeListFile:         filepath.Join(dataDir, "node_list.json"),
		taskMapFile:          filepath.Join(dataDir, "task_map.json"),
		domainIPMappingsFile: filepath.Join(dataDir, "domain_ip_mappings.json"),
		cacheManager:         NewCacheManager(),
	}
	fm.loadFiles()
	return fm, nil
}

func (fm *FileManager) loadFiles() {
	if data, err := os.ReadFile(fm.nodeListFile); err == nil {
		var nodeList pb.NodeList
		if err := json.Unmarshal(data, &nodeList); err == nil {
			fm.cacheManager.SetNodeList(&nodeList)
			fm.cacheManager.SetHash(fm.nodeListFile, CalculateMD5(data))
		}
	}

	if data, err := os.ReadFile(fm.taskMapFile); err == nil {
		var taskMap map[string][]*pb.ProbeTask
		if err := json.Unmarshal(data, &taskMap); err == nil {
			fm.cacheManager.SetAllTasks(taskMap)

			for ip, tasks := range taskMap {
				nodeTaskFile := filepath.Join(fm.dataDir, fmt.Sprintf("tasks_%s.json", ip))
				taskData, _ := json.MarshalIndent(tasks, "", "  ")
				os.WriteFile(nodeTaskFile, taskData, 0644)
				fm.cacheManager.SetHash(nodeTaskFile, CalculateMD5(taskData))
			}
		}
	}

	if data, err := os.ReadFile(fm.domainIPMappingsFile); err == nil {
		var mappings []*pb.DomainIPMapping
		if err := json.Unmarshal(data, &mappings); err == nil {
			fm.cacheManager.SetDomainIPMappings(mappings)
			fm.cacheManager.SetHash(fm.domainIPMappingsFile, CalculateMD5(data))
		}
	}
}

func (fm *FileManager) SaveNodeList(nodeList *pb.NodeList) error {
	data, err := json.MarshalIndent(nodeList, "", "  ")
	if err != nil {
		return fmt.Errorf(": %v", err)
	}

	if err := os.WriteFile(fm.nodeListFile, data, 0644); err != nil {
		return fmt.Errorf(": %v", err)
	}

	fm.cacheManager.SetNodeList(nodeList)
	fm.cacheManager.SetHash(fm.nodeListFile, CalculateMD5(data))

	return nil
}

func (fm *FileManager) SaveNodeTasks(ip string, tasks []*pb.ProbeTask) error {
	fm.cacheManager.SetTasks(ip, tasks)

	allTasks := fm.cacheManager.GetAllTasks()
	taskMapData, err := json.MarshalIndent(allTasks, "", "  ")
	if err != nil {
		return fmt.Errorf(": %v", err)
	}

	if err := os.WriteFile(fm.taskMapFile, taskMapData, 0644); err != nil {
		return fmt.Errorf(": %v", err)
	}

	nodeTaskFile := filepath.Join(fm.dataDir, fmt.Sprintf("tasks_%s.json", ip))
	taskData, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf(": %v", err)
	}

	if err := os.WriteFile(nodeTaskFile, taskData, 0644); err != nil {
		return fmt.Errorf(": %v", err)
	}

	fm.cacheManager.SetHash(nodeTaskFile, CalculateMD5(taskData))

	return nil
}

func (fm *FileManager) SaveDomainIPMappings(mappings []*pb.DomainIPMapping) error {
	data, err := json.MarshalIndent(mappings, "", "  ")
	if err != nil {
		return fmt.Errorf("-IP: %v", err)
	}

	if err := os.WriteFile(fm.domainIPMappingsFile, data, 0644); err != nil {
		return fmt.Errorf("-IP: %v", err)
	}

	fm.cacheManager.SetDomainIPMappings(mappings)
	fm.cacheManager.SetHash(fm.domainIPMappingsFile, CalculateMD5(data))

	return nil
}

func (fm *FileManager) GetNodeList() *pb.NodeList {
	return fm.cacheManager.GetNodeList()
}

func (fm *FileManager) GetNodeTasks(ip string) []*pb.ProbeTask {
	return fm.cacheManager.GetTasks(ip)
}

func (fm *FileManager) GetDomainIPMappings() []*pb.DomainIPMapping {
	return fm.cacheManager.GetDomainIPMappings()
}

func (fm *FileManager) GetNodeListHash() string {
	return fm.cacheManager.GetHash(fm.nodeListFile)
}

func (fm *FileManager) GetNodeTasksHash(ip string) string {
	nodeTaskFile := filepath.Join(fm.dataDir, fmt.Sprintf("tasks_%s.json", ip))
	return fm.cacheManager.GetHash(nodeTaskFile)
}

func (fm *FileManager) GetDomainIPMappingsHash() string {
	return fm.cacheManager.GetHash(fm.domainIPMappingsFile)
}
