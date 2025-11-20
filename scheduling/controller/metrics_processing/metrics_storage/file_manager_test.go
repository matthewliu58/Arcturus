package metrics_storage

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	pb "scheduling/controller/metrics_processing/protocol"
	"testing"
	"time"
)

func TestCacheManager(t *testing.T) {
	fmt.Println("===  TestCacheManager ===")

	cm := NewCacheManager()
	fmt.Println("")

	t.Run("NodeList", func(t *testing.T) {
		fmt.Println("\n---  NodeList  ---")

		nodeList := &pb.NodeList{
			Nodes: []*pb.NodeInfo{
				{Ip: "192.168.1.1", Region: "region-1"},
				{Ip: "192.168.1.2", Region: "region-2"},
			},
		}
		fmt.Println(": NodeList", len(nodeList.Nodes), "")
		for i, node := range nodeList.Nodes {
			fmt.Printf("  [%d]: IP=%s, Region=%s\n", i, node.Ip, node.Region)
		}

		fmt.Println(": SetNodeList()")
		cm.SetNodeList(nodeList)

		fmt.Println(": GetNodeList()")
		result := cm.GetNodeList()

		if !reflect.DeepEqual(nodeList, result) {
			fmt.Println("❌ : GetNodeList() ")
			t.Errorf("GetNodeList() = %v, want %v", result, nodeList)
		} else {
			fmt.Println("✅ : GetNodeList() ")
		}

		fmt.Println("--- NodeList  ---")
	})

	t.Run("Tasks", func(t *testing.T) {
		fmt.Println("\n---  Tasks  ---")

		ip := "192.168.1.1"
		tasks := []*pb.ProbeTask{
			{TaskId: "task1", TargetIp: "10.0.0.1", Timeout: 30},
			{TaskId: "task2", TargetIp: "10.0.0.2", Timeout: 60},
		}
		fmt.Printf(": IP=%s, %d\n", ip, len(tasks))
		for i, task := range tasks {
			fmt.Printf("  [%d]: ID=%s, IP=%s, =%d\n",
				i, task.TaskId, task.TargetIp, task.Timeout)
		}

		fmt.Printf(": SetTasks(%s, metrics_tasks)\n", ip)
		cm.SetTasks(ip, tasks)

		fmt.Printf(": GetTasks(%s)\n", ip)
		result := cm.GetTasks(ip)

		if !reflect.DeepEqual(tasks, result) {
			fmt.Println("❌ : GetTasks() ")
			t.Errorf("GetTasks(%s) = %v, want %v", ip, result, tasks)
		} else {
			fmt.Println("✅ : GetTasks() ")
		}

		fmt.Println("\n: GetAllTasks()")
		allTasks := cm.GetAllTasks()
		fmt.Printf(", %d\n", len(allTasks))

		for nodeIP, nodeTasks := range allTasks {
			fmt.Printf("  IP=%s: %d\n", nodeIP, len(nodeTasks))
		}

		if !reflect.DeepEqual(allTasks[ip], tasks) {
			fmt.Println("❌ : GetAllTasks() ")
			t.Errorf("GetAllTasks()[%s] = %v, want %v", ip, allTasks[ip], tasks)
		} else {
			fmt.Println("✅ : GetAllTasks() ")
		}

		taskMap := map[string][]*pb.ProbeTask{
			"192.168.1.1": tasks,
			"192.168.1.2": {
				{TaskId: "task3", TargetIp: "10.0.0.3", Timeout: 45},
			},
		}
		fmt.Println("\n: ")
		for nodeIP, nodeTasks := range taskMap {
			fmt.Printf("  IP=%s: %d\n", nodeIP, len(nodeTasks))
			for i, task := range nodeTasks {
				fmt.Printf("    [%d]: ID=%s, IP=%s, =%d\n",
					i, task.TaskId, task.TargetIp, task.Timeout)
			}
		}

		fmt.Println(": SetAllTasks(taskMap)")
		cm.SetAllTasks(taskMap)

		fmt.Println(": GetAllTasks()")
		allTasksResult := cm.GetAllTasks()
		fmt.Printf(", %d\n", len(allTasksResult))

		if !reflect.DeepEqual(taskMap, allTasksResult) {
			fmt.Println("❌ : , GetAllTasks() ")
			t.Errorf("GetAllTasks() = %v, want %v", allTasksResult, taskMap)
		} else {
			fmt.Println("✅ : , GetAllTasks() ")
		}

		fmt.Println("--- Tasks  ---")
	})

	t.Run("DomainIPMappings", func(t *testing.T) {
		fmt.Println("\n---  DomainIPMappings  ---")

		mappings := []*pb.DomainIPMapping{
			{Domain: "example.com", Ip: "93.184.216.34"},
			{Domain: "google.com", Ip: "142.250.190.78"},
		}
		fmt.Printf(": %dIP\n", len(mappings))
		for i, mapping := range mappings {
			fmt.Printf("  [%d]: =%s, IP=%s\n", i, mapping.Domain, mapping.Ip)
		}

		fmt.Println(": SetDomainIPMappings(mappings)")
		cm.SetDomainIPMappings(mappings)

		fmt.Println(": GetDomainIPMappings()")
		result := cm.GetDomainIPMappings()
		fmt.Printf("IP, %d\n", len(result))

		if !reflect.DeepEqual(mappings, result) {
			fmt.Println("❌ : GetDomainIPMappings() ")
			t.Errorf("GetDomainIPMappings() = %v, want %v", result, mappings)
		} else {
			fmt.Println("✅ : GetDomainIPMappings() ")
		}

		fmt.Println("--- DomainIPMappings  ---")
	})

	t.Run("HashCache", func(t *testing.T) {
		fmt.Println("\n---  HashCache  ---")

		filePath := "/tmp/test.txt"
		hash := "d41d8cd98f00b204e9800998ecf8427e"
		fmt.Printf(": =%s, =%s\n", filePath, hash)

		fmt.Printf(": SetHash(%s, %s)\n", filePath, hash)
		cm.SetHash(filePath, hash)

		fmt.Printf(": GetHash(%s)\n", filePath)
		result := cm.GetHash(filePath)
		fmt.Printf(": %s\n", result)

		if result != hash {
			fmt.Println("❌ : GetHash() ")
			t.Errorf("GetHash(%s) = %s, want %s", filePath, result, hash)
		} else {
			fmt.Println("✅ : GetHash() ")
		}

		fmt.Println("--- HashCache  ---")
	})

	fmt.Println("\n=== TestCacheManager  ===")
}

func TestFileManager(t *testing.T) {
	fmt.Println("===  TestFileManager ===")

	fmt.Println("...")
	tempDir, err := ioutil.TempDir("", "test-metrics_storage")
	if err != nil {
		fmt.Printf("❌ : %v\n", err)
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	fmt.Printf("✅ : %s\n", tempDir)
	defer func() {
		fmt.Printf(": %s\n", tempDir)
		os.RemoveAll(tempDir)
	}()

	fmt.Printf(", : %s\n", tempDir)
	fm, err := NewFileManager(tempDir)
	if err != nil {
		fmt.Printf("❌ : %v\n", err)
		t.Fatalf("NewFileManager() error = %v", err)
	}
	fmt.Println("✅ ")

	t.Run("NodeList", func(t *testing.T) {
		fmt.Println("\n---  NodeList  ---")

		nodeList := &pb.NodeList{
			Nodes: []*pb.NodeInfo{
				{Ip: "192.168.1.1", Region: "region-1"},
				{Ip: "192.168.1.2", Region: "region-2"},
			},
		}
		fmt.Println(": NodeList", len(nodeList.Nodes), "")
		for i, node := range nodeList.Nodes {
			fmt.Printf("  [%d]: IP=%s, Region=%s\n", i, node.Ip, node.Region)
		}

		fmt.Println(": SaveNodeList()")
		if err := fm.SaveNodeList(nodeList); err != nil {
			fmt.Printf("❌ : %v\n", err)
			t.Errorf("SaveNodeList() error = %v", err)
		} else {
			fmt.Println("✅ ")
		}

		nodeListFile := filepath.Join(tempDir, "node_list.json")
		fmt.Printf(": %s\n", nodeListFile)
		if _, err := os.Stat(nodeListFile); os.IsNotExist(err) {
			fmt.Println("❌ ")
			t.Errorf("Node list file was not created")
		} else {
			fmt.Println("✅ ")
		}

		fmt.Printf(": %s\n", nodeListFile)
		data, err := ioutil.ReadFile(nodeListFile)
		if err != nil {
			fmt.Printf("❌ : %v\n", err)
			t.Errorf("Failed to read node list file: %v", err)
		} else {
			fmt.Printf("✅ %d\n", len(data))
		}

		fmt.Println("JSON")
		var savedNodeList pb.NodeList
		if err := json.Unmarshal(data, &savedNodeList); err != nil {
			fmt.Printf("❌ JSON: %v\n", err)
			t.Errorf("Failed to unmarshal node list: %v", err)
		} else {
			fmt.Printf("✅ JSON, %d\n", len(savedNodeList.Nodes))
		}

		if !reflect.DeepEqual(&savedNodeList, nodeList) {
			fmt.Println("❌ ")
			t.Errorf("Saved node list = %v, want %v", savedNodeList, nodeList)
		} else {
			fmt.Println("✅ : ")
		}

		fmt.Println(": GetNodeList()")
		result := fm.GetNodeList()
		if result == nil {
			fmt.Println("❌ GetNodeList() ")
		} else {
			fmt.Printf("✅ GetNodeList() , %d\n", len(result.Nodes))
		}

		if !reflect.DeepEqual(result, nodeList) {
			fmt.Println("❌ GetNodeList() ")
			t.Errorf("GetNodeList() = %v, want %v", result, nodeList)
		} else {
			fmt.Println("✅ : GetNodeList() ")
		}

		fmt.Println(": GetNodeListHash()")
		hash := fm.GetNodeListHash()
		fmt.Printf(": %s\n", hash)

		expectedHash := CalculateMD5(data)
		fmt.Printf(": %s\n", expectedHash)

		if hash != expectedHash {
			fmt.Println("❌ GetNodeListHash() ")
			t.Errorf("GetNodeListHash() = %s, want %s", hash, expectedHash)
		} else {
			fmt.Println("✅ : GetNodeListHash() ")
		}

		fmt.Println("--- NodeList  ---")
	})

	t.Run("NodeTasks", func(t *testing.T) {
		fmt.Println("\n---  NodeTasks  ---")

		ip := "192.168.1.1"
		tasks := []*pb.ProbeTask{
			{TaskId: "task1", TargetIp: "10.0.0.1", Timeout: 30},
			{TaskId: "task2", TargetIp: "10.0.0.2", Timeout: 60},
		}
		fmt.Printf(": IP=%s, %d\n", ip, len(tasks))
		for i, task := range tasks {
			fmt.Printf("  [%d]: ID=%s, IP=%s, =%d\n",
				i, task.TaskId, task.TargetIp, task.Timeout)
		}

		fmt.Printf(": SaveNodeTasks(%s)\n", ip)
		if err := fm.SaveNodeTasks(ip, tasks); err != nil {
			fmt.Printf("❌ : %v\n", err)
			t.Errorf("SaveNodeTasks() error = %v", err)
		} else {
			fmt.Println("✅ ")
		}

		taskMapFile := filepath.Join(tempDir, "task_map.json")
		fmt.Printf(": %s\n", taskMapFile)
		if _, err := os.Stat(taskMapFile); os.IsNotExist(err) {
			fmt.Println("❌ ")
			t.Errorf("Task map file was not created")
		} else {
			fmt.Println("✅ ")

			data, err := ioutil.ReadFile(taskMapFile)
			if err == nil {
				fmt.Printf(" (%d ):\n", len(data))
				fmt.Println(string(data))
			}
		}

		nodeTaskFile := filepath.Join(tempDir, "tasks_"+ip+".json")
		fmt.Printf(": %s\n", nodeTaskFile)
		if _, err := os.Stat(nodeTaskFile); os.IsNotExist(err) {
			fmt.Println("❌ ")
			t.Errorf("Node task file was not created")
		} else {
			fmt.Println("✅ ")

			data, err := ioutil.ReadFile(nodeTaskFile)
			if err == nil {
				fmt.Printf(" (%d ):\n", len(data))
				fmt.Println(string(data))
			}
		}

		fmt.Printf(": GetNodeTasks(%s)\n", ip)
		result := fm.GetNodeTasks(ip)
		if result == nil {
			fmt.Println("❌ GetNodeTasks() ")
		} else {
			fmt.Printf("✅ GetNodeTasks() , %d\n", len(result))
			for i, task := range result {
				fmt.Printf("  [%d]: ID=%s, IP=%s, =%d\n",
					i, task.TaskId, task.TargetIp, task.Timeout)
			}
		}

		if !reflect.DeepEqual(result, tasks) {
			fmt.Println("❌ GetNodeTasks() ")
			t.Errorf("GetNodeTasks(%s) = %v, want %v", ip, result, tasks)
		} else {
			fmt.Println("✅ : GetNodeTasks() ")
		}

		fmt.Printf(": GetNodeTasksHash(%s)\n", ip)
		hash := fm.GetNodeTasksHash(ip)
		fmt.Printf(": %s\n", hash)

		data, _ := ioutil.ReadFile(nodeTaskFile)
		expectedHash := CalculateMD5(data)
		fmt.Printf(": %s\n", expectedHash)

		if hash != expectedHash {
			fmt.Println("❌ GetNodeTasksHash() ")
			t.Errorf("GetNodeTasksHash(%s) = %s, want %s", ip, hash, expectedHash)
		} else {
			fmt.Println("✅ : GetNodeTasksHash() ")
		}

		fmt.Println("--- NodeTasks  ---")
	})

	t.Run("DomainIPMappings", func(t *testing.T) {
		fmt.Println("\n---  DomainIPMappings  ---")

		mappings := []*pb.DomainIPMapping{
			{Domain: "example.com", Ip: "93.184.216.34"},
			{Domain: "google.com", Ip: "142.250.190.78"},
		}
		fmt.Printf(": %dIP\n", len(mappings))
		for i, mapping := range mappings {
			fmt.Printf("  [%d]: =%s, IP=%s\n", i, mapping.Domain, mapping.Ip)
		}

		fmt.Println(": SaveDomainIPMappings()")
		if err := fm.SaveDomainIPMappings(mappings); err != nil {
			fmt.Printf("❌ IP: %v\n", err)
			t.Errorf("SaveDomainIPMappings() error = %v", err)
		} else {
			fmt.Println("✅ IP")
		}

		mappingsFile := filepath.Join(tempDir, "domain_ip_mappings.json")
		fmt.Printf("IP: %s\n", mappingsFile)
		if _, err := os.Stat(mappingsFile); os.IsNotExist(err) {
			fmt.Println("❌ IP")
			t.Errorf("Domain IP mappings file was not created")
		} else {
			fmt.Println("✅ IP")

			data, err := ioutil.ReadFile(mappingsFile)
			if err == nil {
				fmt.Printf("IP (%d ):\n", len(data))
				fmt.Println(string(data))
			}
		}

		fmt.Println(": GetDomainIPMappings()")
		result := fm.GetDomainIPMappings()
		if result == nil {
			fmt.Println("❌ GetDomainIPMappings() ")
		} else {
			fmt.Printf("✅ GetDomainIPMappings() , %d\n", len(result))
			for i, mapping := range result {
				fmt.Printf("  [%d]: =%s, IP=%s\n", i, mapping.Domain, mapping.Ip)
			}
		}

		if !reflect.DeepEqual(result, mappings) {
			fmt.Println("❌ GetDomainIPMappings() ")
			t.Errorf("GetDomainIPMappings() = %v, want %v", result, mappings)
		} else {
			fmt.Println("✅ : GetDomainIPMappings() ")
		}

		fmt.Println(": GetDomainIPMappingsHash()")
		hash := fm.GetDomainIPMappingsHash()
		fmt.Printf(": %s\n", hash)

		data, _ := ioutil.ReadFile(mappingsFile)
		expectedHash := CalculateMD5(data)
		fmt.Printf(": %s\n", expectedHash)

		if hash != expectedHash {
			fmt.Println("❌ GetDomainIPMappingsHash() ")
			t.Errorf("GetDomainIPMappingsHash() = %s, want %s", hash, expectedHash)
		} else {
			fmt.Println("✅ : GetDomainIPMappingsHash() ")
		}

		fmt.Println("--- DomainIPMappings  ---")
	})

	fmt.Println("\n=== TestFileManager  ===")
}

func TestHash(t *testing.T) {
	fmt.Println("===  (TestHash) ===")

	t.Run("CalculateMD5", func(t *testing.T) {
		fmt.Println("\n---  CalculateMD5  ---")

		data := []byte("hello world")
		fmt.Printf(": %q (: %d)\n", string(data), len(data))

		expectedHash := "5eb63bbbe01eeed093cb22bb8f5acdc3"
		fmt.Printf(": %s\n", expectedHash)

		fmt.Println(": CalculateMD5(data)")
		startTime := time.Now()
		hash := CalculateMD5(data)
		duration := time.Since(startTime)
		fmt.Printf(": %v\n", duration)
		fmt.Printf(": %s\n", hash)

		if hash != expectedHash {
			fmt.Println("❌ : ")
			t.Errorf("CalculateMD5() = %s, want %s", hash, expectedHash)
		} else {
			fmt.Println("✅ : ")
		}

		fmt.Println("\n: MD5")

		input1 := []byte("hello world")
		input2 := []byte("hello world.") //
		hash1 := CalculateMD5(input1)
		hash2 := CalculateMD5(input2)
		fmt.Printf(" %q MD5: %s\n", string(input1), hash1)
		fmt.Printf(" %q MD5: %s\n", string(input2), hash2)
		fmt.Println(": ，")

		fmt.Println("--- CalculateMD5  ---")
	})

	t.Run("CalculateFileHash", func(t *testing.T) {
		fmt.Println("\n---  CalculateFileHash  ---")

		fmt.Println("...")
		tempFile, err := ioutil.TempFile("", "test-hash")
		if err != nil {
			fmt.Printf("❌ : %v\n", err)
			t.Fatalf("Failed to create temp file: %v", err)
		}
		fmt.Printf("✅ : %s\n", tempFile.Name())

		defer func() {
			fmt.Printf(":  %s\n", tempFile.Name())
			os.Remove(tempFile.Name())
		}()

		testData := []byte("test data for hash calculation")
		fmt.Printf(": %q (: %d)\n", string(testData), len(testData))

		writeStart := time.Now()
		bytesWritten, err := tempFile.Write(testData)
		writeDuration := time.Since(writeStart)

		if err != nil {
			fmt.Printf("❌ : %v\n", err)
			t.Fatalf("Failed to write to temp file: %v", err)
		}
		fmt.Printf("✅  %d ，: %v\n", bytesWritten, writeDuration)

		fmt.Println("...")
		if err := tempFile.Close(); err != nil {
			fmt.Printf("❌ : %v\n", err)
			t.Fatalf("Failed to close temp file: %v", err)
		}
		fmt.Println("✅ ")

		fileInfo, err := os.Stat(tempFile.Name())
		if err != nil {
			fmt.Printf("❌ : %v\n", err)
		} else {
			fmt.Printf(": =%d, =%s\n",
				fileInfo.Size(), fileInfo.ModTime().Format(time.RFC3339))
		}

		fmt.Println("\nMD5...")
		memoryHashStart := time.Now()
		expectedHash := CalculateMD5(testData)
		memoryHashDuration := time.Since(memoryHashStart)
		fmt.Printf(": %s (: %v)\n", expectedHash, memoryHashDuration)

		fmt.Printf("\n: CalculateFileHash(%s)\n", tempFile.Name())
		fileHashStart := time.Now()
		hash, err := CalculateFileHash(tempFile.Name())
		fileHashDuration := time.Since(fileHashStart)

		if err != nil {
			fmt.Printf("❌ : %v\n", err)
			t.Errorf("CalculateFileHash() error = %v", err)
		} else {
			fmt.Printf("✅ : %s (: %v)\n", hash, fileHashDuration)
		}

		fmt.Println("\n:")
		fmt.Printf(": %s\n", expectedHash)
		fmt.Printf(": %s\n", hash)

		if hash != expectedHash {
			fmt.Println("❌ : ")
			t.Errorf("CalculateFileHash() = %s, want %s", hash, expectedHash)
		} else {
			fmt.Println("✅ : ")
		}

		fmt.Println("\n:")
		fmt.Println("1. （）")
		fmt.Println("2. （）")
		fmt.Println("3. （）")
		fmt.Println("4. （）")
		fmt.Println("")

		fmt.Println("--- CalculateFileHash  ---")
	})

	fmt.Println("\n===  (TestHash)  ===")
}

func TestIntegration(t *testing.T) {
	fmt.Println("===============================================")
	fmt.Println("===  (TestIntegration) ===")
	fmt.Println("===============================================")

	fmt.Println("\n[1] ")
	fmt.Println("...")
	tempDir, err := ioutil.TempDir("", "test-integration")
	if err != nil {
		fmt.Printf("❌ : %v\n", err)
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	fmt.Printf("✅ : %s\n", tempDir)
	defer func() {
		fmt.Printf("\n: %s\n", tempDir)
		os.RemoveAll(tempDir)
	}()

	fmt.Printf("\n[2] \n")
	fmt.Printf(", : %s\n", tempDir)
	fm, err := NewFileManager(tempDir)
	if err != nil {
		fmt.Printf("❌ : %v\n", err)
		t.Fatalf("NewFileManager() error = %v", err)
	}
	fmt.Println("✅ ")

	fmt.Println("\n[3] ")

	fmt.Println("\n[3.1] ")
	nodeList := &pb.NodeList{
		Nodes: []*pb.NodeInfo{
			{Ip: "192.168.1.1", Region: "region-1"},
			{Ip: "192.168.1.2", Region: "region-2"},
		},
	}
	fmt.Printf(" %d :\n", len(nodeList.Nodes))
	for i, node := range nodeList.Nodes {
		fmt.Printf("  [%d]: IP=%s, Region=%s\n", i, node.Ip, node.Region)
	}

	fmt.Println("\n[3.2] ")
	tasks := map[string][]*pb.ProbeTask{
		"192.168.1.1": {
			{TaskId: "task1", TargetIp: "10.0.0.1", Timeout: 30},
			{TaskId: "task2", TargetIp: "10.0.0.2", Timeout: 60},
		},
		"192.168.1.2": {
			{TaskId: "task3", TargetIp: "10.0.0.3", Timeout: 45},
		},
	}
	fmt.Printf(" %d :\n", len(tasks))
	for ip, nodeTasks := range tasks {
		fmt.Printf("  IP=%s:  %d \n", ip, len(nodeTasks))
		for j, task := range nodeTasks {
			fmt.Printf("    [%d]: ID=%s, IP=%s, =%d\n",
				j, task.TaskId, task.TargetIp, task.Timeout)
		}
	}

	fmt.Println("\n[3.3] IP")
	mappings := []*pb.DomainIPMapping{
		{Domain: "example.com", Ip: "93.184.216.34"},
		{Domain: "google.com", Ip: "142.250.190.78"},
	}
	fmt.Printf("IP %d :\n", len(mappings))
	for i, mapping := range mappings {
		fmt.Printf("  [%d]: =%s, IP=%s\n", i, mapping.Domain, mapping.Ip)
	}

	fmt.Println("\n[4] ")
	fmt.Println("\n[4.1] ")
	if err := fm.SaveNodeList(nodeList); err != nil {
		fmt.Printf("❌ : %v\n", err)
		t.Errorf("SaveNodeList() error = %v", err)
	} else {
		fmt.Println("✅ ")
		nodeListFile := filepath.Join(tempDir, "node_list.json")
		info, err := os.Stat(nodeListFile)
		if err == nil {
			fmt.Printf("  : %s\n", nodeListFile)
			fmt.Printf("  : %d \n", info.Size())
			fmt.Printf("  : %s\n", info.ModTime())
		}
	}

	fmt.Println("\n[4.2] ")
	for ip, nodeTasks := range tasks {
		fmt.Printf(" %s ...\n", ip)
		if err := fm.SaveNodeTasks(ip, nodeTasks); err != nil {
			fmt.Printf("❌ : %v\n", err)
			t.Errorf("SaveNodeTasks(%s) error = %v", ip, err)
		} else {
			fmt.Printf("✅  %s \n", ip)
			//
			taskFile := filepath.Join(tempDir, "tasks_"+ip+".json")
			info, err := os.Stat(taskFile)
			if err == nil {
				fmt.Printf("  : %s\n", taskFile)
				fmt.Printf("  : %d \n", info.Size())
			}
		}
	}

	taskMapFile := filepath.Join(tempDir, "task_map.json")
	if _, err := os.Stat(taskMapFile); err == nil {
		fmt.Printf("✅ : %s\n", taskMapFile)
		data, err := ioutil.ReadFile(taskMapFile)
		if err == nil {
			fmt.Printf("  : %d \n", len(data))
			fmt.Printf("  :\n%s\n", string(data))
		}
	} else {
		fmt.Printf("❌ : %v\n", err)
	}

	fmt.Println("\n[4.3] IP")
	if err := fm.SaveDomainIPMappings(mappings); err != nil {
		fmt.Printf("❌ IP: %v\n", err)
		t.Errorf("SaveDomainIPMappings() error = %v", err)
	} else {
		fmt.Println("✅ IP")
		mappingsFile := filepath.Join(tempDir, "domain_ip_mappings.json")
		info, err := os.Stat(mappingsFile)
		if err == nil {
			fmt.Printf("  : %s\n", mappingsFile)
			fmt.Printf("  : %d \n", info.Size())
		}
	}

	fmt.Println("\n[5] ")
	files, err := ioutil.ReadDir(tempDir)
	if err != nil {
		fmt.Printf("❌ : %v\n", err)
	} else {
		fmt.Printf(" %s :\n", tempDir)
		for i, file := range files {
			fmt.Printf("  [%d] %s (%d )\n", i+1, file.Name(), file.Size())
		}
	}

	fmt.Println("\n[6] ")
	fmt.Println("...")
	newFm, err := NewFileManager(tempDir)
	if err != nil {
		fmt.Printf("❌ : %v\n", err)
		t.Fatalf("NewFileManager() error = %v", err)
	}
	fmt.Println("✅ ")

	fmt.Println("\n[7] ")

	fmt.Println("\n[7.1] ")
	loadedNodeList := newFm.GetNodeList()
	if loadedNodeList == nil {
		fmt.Println("❌ ")
	} else {
		fmt.Printf("✅ ,  %d \n", len(loadedNodeList.Nodes))
		for i, node := range loadedNodeList.Nodes {
			fmt.Printf("  [%d]: IP=%s, Region=%s\n", i, node.Ip, node.Region)
		}
	}

	if !reflect.DeepEqual(newFm.GetNodeList(), nodeList) {
		fmt.Println("❌ ")
		t.Errorf("After reload, GetNodeList() = %v, want %v", newFm.GetNodeList(), nodeList)
	} else {
		fmt.Println("✅ : ")
	}

	fmt.Println("\n[7.2] ")
	for ip, expectedTasks := range tasks {
		fmt.Printf(" %s ...\n", ip)
		loadedTasks := newFm.GetNodeTasks(ip)

		if loadedTasks == nil {
			fmt.Printf("❌  %s \n", ip)
		} else {
			fmt.Printf("✅  %s ,  %d \n", ip, len(loadedTasks))
			for j, task := range loadedTasks {
				fmt.Printf("  [%d]: ID=%s, IP=%s, =%d\n",
					j, task.TaskId, task.TargetIp, task.Timeout)
			}
		}

		if !reflect.DeepEqual(newFm.GetNodeTasks(ip), expectedTasks) {
			fmt.Printf("❌  %s \n", ip)
			t.Errorf("After reload, GetNodeTasks(%s) = %v, want %v", ip, newFm.GetNodeTasks(ip), expectedTasks)
		} else {
			fmt.Printf("✅ :  %s \n", ip)
		}
	}

	fmt.Println("\n[7.3] IP")
	loadedMappings := newFm.GetDomainIPMappings()
	if loadedMappings == nil {
		fmt.Println("❌ IP")
	} else {
		fmt.Printf("✅ IP,  %d \n", len(loadedMappings))
		for i, mapping := range loadedMappings {
			fmt.Printf("  [%d]: =%s, IP=%s\n", i, mapping.Domain, mapping.Ip)
		}
	}

	if !reflect.DeepEqual(newFm.GetDomainIPMappings(), mappings) {
		fmt.Println("❌ IP")
		t.Errorf("After reload, GetDomainIPMappings() = %v, want %v", newFm.GetDomainIPMappings(), mappings)
	} else {
		fmt.Println("✅ : IP")
	}

	fmt.Println("\n===============================================")
	fmt.Println("===  (TestIntegration) ===")
	fmt.Println("===============================================")
}
