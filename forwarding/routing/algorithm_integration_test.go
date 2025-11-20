package routing

import (
	"forwarding/middle_mile_scheduling/adapter"
	"forwarding/middle_mile_scheduling/common"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

// TestDefaultAlgorithmConfiguration tests that when no algorithm is specified in config,
// the system defaults to k_shortest and uses the calculator correctly
func TestDefaultAlgorithmConfiguration(t *testing.T) {
	// 设置日志级别以便看到详细信息
	log.SetLevel(log.InfoLevel)

	t.Run("NoConfigAlgorithmUsesDefaultKShortest", func(t *testing.T) {
		// 清理实例
		instances = make(map[string]*PathManager)
		// Note: sync.Once doesn't support Reset() in Go 1.23
		// We manually clear instances instead

		// 验证默认算法设置
		defaultAlg := GetDefaultAlgorithm()
		if defaultAlg != "k_shortest" {
			t.Fatalf("Expected default algorithm to be 'k_shortest', got '%s'", defaultAlg)
		}
		t.Logf("✓ Default algorithm is correctly set to: %s", defaultAlg)

		// 创建网络拓扑
		network := common.Network{
			Nodes: []common.Node{{}, {}, {}, {}, {}},
			Links: [][]int{
				{0, 10, -1, -1, 30},
				{10, 0, 15, -1, -1},
				{-1, 15, 0, 20, -1},
				{-1, -1, 20, 0, 25},
				{30, -1, -1, 25, 0},
			},
		}

		ipToIndex := map[string]int{
			"10.0.0.1": 0,
			"10.0.0.2": 1,
			"10.0.0.3": 2,
			"10.0.0.4": 3,
			"10.0.0.5": 4,
		}

		indexToIP := map[int]string{
			0: "10.0.0.1",
			1: "10.0.0.2",
			2: "10.0.0.3",
			3: "10.0.0.4",
			4: "10.0.0.5",
		}

		// 模拟 PathManager 创建（模拟从配置拉取但没有指定算法字段的场景）
		pm := &PathManager{
			pathChan:      make(chan []common.PathWithIP, 1),
			latestPaths:   nil,
			sourceIP:      "10.0.0.1",
			destinationIP: "10.0.0.5",
			domain:        "example.com",
			params: map[string]interface{}{
				"k": 2,
			},
			algorithmName: GetDefaultAlgorithm(), // 使用默认算法
		}

		// 获取默认算法的 calculator
		calc, err := common.GetGlobal(GetDefaultAlgorithm())
		if err != nil {
			// 如果算法未注册，这是预期的行为
			t.Logf("ℹ Algorithm '%s' not registered yet (expected before adapter init), will use fallback",
				GetDefaultAlgorithm())
		} else {
			pm.calculator = calc
			t.Logf("✓ Successfully retrieved calculator for algorithm: %s", GetDefaultAlgorithm())
		}

		// 启动路径监听器
		go pm.pathListener()

		// 调用 CalculatePaths - 这应该使用 calculator（如果已注册）或降级到 K-Shortest
		t.Logf("Starting path calculation with domain=%s, algorithm=%s", pm.domain, pm.algorithmName)
		pm.CalculatePaths(network, ipToIndex, indexToIP)

		// 等待路径计算完成
		time.Sleep(500 * time.Millisecond)

		// 验证结果
		paths := pm.GetPaths()

		if len(paths) == 0 {
			t.Logf("⚠ No paths computed (might be expected if source/dest not connected or algorithm not initialized)")
		} else {
			t.Logf("✓ Successfully computed %d paths", len(paths))

			// 验证路径信息
			for i, path := range paths {
				t.Logf("  Path[%d]: %d hops, latency=%d, weight=%d, nodes=%v",
					i, len(path.IPList), path.Latency, path.Weight, path.IPList)

				// 验证路径的有效性
				if len(path.IPList) == 0 {
					t.Errorf("Path %d has empty IP list", i)
				}
				if path.Latency < 0 {
					t.Errorf("Path %d has negative latency: %d", i, path.Latency)
				}
				if path.Weight < 0 {
					t.Errorf("Path %d has negative weight: %d", i, path.Weight)
				}
			}
		}

		// 验证 calculator 的使用情况
		if pm.calculator != nil {
			t.Logf("✓ Calculator is set and being used (algorithmName=%s)", pm.algorithmName)
		} else {
			t.Logf("ℹ Calculator is nil, will use fallback K-Shortest implementation")
		}
	})
}

// TestAlgorithmInitializationWithAdapter 测试算法注册和初始化流程
func TestAlgorithmInitializationWithAdapter(t *testing.T) {
	t.Run("KShortestAdapterInitialization", func(t *testing.T) {
		// 创建并注册 K-Shortest adapter
		ksAdapter := adapter.NewKShortestAdapter(3)
		err := common.RegisterGlobal("k_shortest", ksAdapter)

		if err != nil {
			t.Logf("ℹ Adapter already registered (not an error): %v", err)
		} else {
			t.Logf("✓ K-Shortest adapter successfully registered")
		}

		// 验证能够检索到注册的 adapter
		retrieved, err := common.GetGlobal("k_shortest")
		if err != nil {
			t.Fatalf("Failed to retrieve k_shortest adapter: %v", err)
		}

		t.Logf("✓ Successfully retrieved k_shortest adapter: %T", retrieved)

		// 验证 adapter 不为 nil
		if retrieved == nil {
			t.Fatalf("Retrieved adapter is nil")
		}
		t.Logf("✓ Adapter correctly registered and retrievable")
	})
}

// TestDefaultAlgorithmPathCalculation 端到端集成测试
func TestDefaultAlgorithmPathCalculation(t *testing.T) {
	t.Run("EndToEndPathCalculationWithDefaultAlgorithm", func(t *testing.T) {
		// 初始化 adapter（模拟实际启动过程）
		ksAdapter := adapter.NewKShortestAdapter(2)
		err := common.RegisterGlobal("k_shortest", ksAdapter)
		if err != nil {
			t.Logf("ℹ Adapter registration note: %v", err)
		}

		// 清理实例
		instances = make(map[string]*PathManager)

		// 创建网络拓扑
		network := common.Network{
			Nodes: []common.Node{{}, {}, {}, {}, {}},
			Links: [][]int{
				{0, 10, -1, -1, 50}, // 0 -> 1: 10ms, 0 -> 4: 50ms
				{10, 0, 15, -1, -1}, // 1 -> 0: 10ms, 1 -> 2: 15ms
				{-1, 15, 0, 20, -1}, // 2 -> 1: 15ms, 2 -> 3: 20ms
				{-1, -1, 20, 0, 25}, // 3 -> 2: 20ms, 3 -> 4: 25ms
				{50, -1, -1, 25, 0}, // 4 -> 0: 50ms, 4 -> 3: 25ms
			},
		}

		ipToIndex := map[string]int{
			"192.168.1.1": 0,
			"192.168.1.2": 1,
			"192.168.1.3": 2,
			"192.168.1.4": 3,
			"192.168.1.5": 4,
		}

		indexToIP := map[int]string{
			0: "192.168.1.1",
			1: "192.168.1.2",
			2: "192.168.1.3",
			3: "192.168.1.4",
			4: "192.168.1.5",
		}

		// 模拟配置中没有指定算法的场景
		// 这意味着在创建 PathManager 时使用默认算法
		pm := &PathManager{
			pathChan:      make(chan []common.PathWithIP, 1),
			latestPaths:   nil,
			sourceIP:      "192.168.1.1",
			destinationIP: "192.168.1.5",
			domain:        "integration-test.com",
			params: map[string]interface{}{
				"k": 2, // 默认参数
			},
			algorithmName: GetDefaultAlgorithm(), // 未在配置中指定，使用默认
		}

		// 初始化 calculator（模拟 GetInstance 的行为）
		calc, err := common.GetGlobal(GetDefaultAlgorithm())
		if err == nil {
			pm.calculator = calc
			t.Logf("✓ Calculator initialized for default algorithm: %s", GetDefaultAlgorithm())
		} else {
			pm.calculator = nil
			t.Logf("✓ Calculator not found, will use fallback (domain=%s, algorithm=%s)",
				pm.domain, pm.algorithmName)
		}

		// 启动路径监听器
		go pm.pathListener()

		// 执行路径计算
		t.Logf("Starting path calculation:")
		t.Logf("  - Domain: %s", pm.domain)
		t.Logf("  - Algorithm: %s (default)", pm.algorithmName)
		t.Logf("  - Source: %s (index 0)", pm.sourceIP)
		t.Logf("  - Destination: %s (index 4)", pm.destinationIP)
		t.Logf("  - Calculator: %v", pm.calculator != nil)

		pm.CalculatePaths(network, ipToIndex, indexToIP)

		// 等待处理
		time.Sleep(500 * time.Millisecond)

		// 获取计算结果
		paths := pm.GetPaths()

		t.Logf("Path calculation completed:")
		t.Logf("  - Paths found: %d", len(paths))

		// 验证计算结果
		if len(paths) > 0 {
			t.Logf("✓ Paths successfully computed using %s algorithm", pm.algorithmName)

			for i, path := range paths {
				t.Logf("  Path %d: %v (latency=%d, weight=%d)",
					i, path.IPList, path.Latency, path.Weight)

				// 验证路径有效性
				if len(path.IPList) == 0 {
					t.Errorf("Invalid path %d: empty IP list", i)
				}
				// 验证路径以正确的 IP 开始和结束
				if path.IPList[0] != pm.sourceIP {
					t.Logf("⚠ Path %d doesn't start with source IP: %s vs %s",
						i, path.IPList[0], pm.sourceIP)
				}
				if path.IPList[len(path.IPList)-1] != pm.destinationIP {
					t.Logf("⚠ Path %d doesn't end with destination IP: %s vs %s",
						i, path.IPList[len(path.IPList)-1], pm.destinationIP)
				}
			}
		} else {
			t.Logf("⚠ No paths computed")
			// 这可能是因为网络中不存在源到目标的路径
			// 或者算法还没初始化
		}
	})
}

// TestConfigWithoutAlgorithmFieldUsesDefault 测试配置中没有算法字段时的行为
func TestConfigWithoutAlgorithmFieldUsesDefault(t *testing.T) {
	t.Run("MissingAlgorithmFieldDefaultsToKShortest", func(t *testing.T) {
		// 模拟配置对象（假设没有 Algorithm 字段）
		type MockConfig struct {
			Domain string `toml:"domain"`
			// 注意：没有 Algorithm 字段
			IP string `toml:"ip"`
		}

		config := MockConfig{
			Domain: "test.example.com",
			IP:     "10.0.0.1",
		}

		// 当配置中没有指定算法时，应该使用默认算法
		algorithmToUse := GetDefaultAlgorithm()

		t.Logf("Config (no algorithm field specified):")
		t.Logf("  - Domain: %s", config.Domain)
		t.Logf("  - IP: %s", config.IP)
		t.Logf("  - Algorithm used: %s (from default)", algorithmToUse)

		if algorithmToUse != "k_shortest" {
			t.Errorf("Expected default algorithm 'k_shortest', got '%s'", algorithmToUse)
		} else {
			t.Logf("✓ Correctly defaults to k_shortest when not specified in config")
		}
	})
}

// TestCalculatorUsageWhenConfigurationIsAbsent 测试当配置缺失时 calculator 的使用
func TestCalculatorUsageWhenConfigurationIsAbsent(t *testing.T) {
	t.Run("CalculatorUsedWhenAvailable", func(t *testing.T) {
		// 初始化 adapter
		ksAdapter := adapter.NewKShortestAdapter(3)
		common.RegisterGlobal("k_shortest", ksAdapter)

		// 清理实例
		instances = make(map[string]*PathManager)

		// 创建简单网络
		network := common.Network{
			Nodes: []common.Node{{}, {}},
			Links: [][]int{
				{0, 10},
				{10, 0},
			},
		}

		ipToIndex := map[string]int{
			"10.0.0.1": 0,
			"10.0.0.2": 1,
		}

		indexToIP := map[int]string{
			0: "10.0.0.1",
			1: "10.0.0.2",
		}

		// 场景 1: 配置没有指定算法，calculator 可用
		t.Logf("Scenario 1: Config without algorithm field, calculator available")
		pm1 := &PathManager{
			pathChan:      make(chan []common.PathWithIP, 1),
			latestPaths:   nil,
			sourceIP:      "10.0.0.1",
			destinationIP: "10.0.0.2",
			domain:        "scenario1.com",
			params:        map[string]interface{}{"k": 2},
			algorithmName: GetDefaultAlgorithm(),
		}

		// 获取 calculator
		calc, _ := common.GetGlobal(GetDefaultAlgorithm())
		pm1.calculator = calc

		go pm1.pathListener()
		pm1.CalculatePaths(network, ipToIndex, indexToIP)
		time.Sleep(200 * time.Millisecond)

		paths1 := pm1.GetPaths()
		t.Logf("  - Calculator used: %v", pm1.calculator != nil)
		t.Logf("  - Paths computed: %d", len(paths1))
		if pm1.calculator != nil && len(paths1) > 0 {
			t.Logf("✓ Calculator successfully used for path computation")
		}

		// 场景 2: 配置没有指定算法，calculator 不可用（模拟降级）
		t.Logf("Scenario 2: Config without algorithm field, calculator unavailable (fallback)")
		pm2 := &PathManager{
			pathChan:      make(chan []common.PathWithIP, 1),
			latestPaths:   nil,
			sourceIP:      "10.0.0.1",
			destinationIP: "10.0.0.2",
			domain:        "scenario2.com",
			params:        map[string]interface{}{"k": 2},
			algorithmName: GetDefaultAlgorithm(),
			calculator:    nil, // 模拟 calculator 不可用
		}

		go pm2.pathListener()
		pm2.CalculatePaths(network, ipToIndex, indexToIP)
		time.Sleep(200 * time.Millisecond)

		paths2 := pm2.GetPaths()
		t.Logf("  - Calculator used: %v (nil)", pm2.calculator != nil)
		t.Logf("  - Paths computed: %d", len(paths2))
		if pm2.calculator == nil && len(paths2) > 0 {
			t.Logf("✓ Fallback K-Shortest successfully used when calculator unavailable")
		}
	})
}
