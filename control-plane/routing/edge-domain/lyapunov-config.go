package edge_domain

// ============== CPU Threshold Configuration ==============

// Default CPU thresholds for 2-core machines
const (
	CPULow  = 40.0
	CPUMid  = 60.0
	CPUHigh = 80.0
)

// Default weight for delay penalty in Lyapunov formula.
//
// Score = 1.0 * cpuPenalty + V * delayPenalty
//
// With V=0.5, CPU dominates most scenarios (see table below).
// If latency sensitivity needs to increase, raise V toward 1.0.
//
//	Scenario          | cpuPenalty | delayPenalty*V | Score | CPU weight
//	------------------|------------|----------------|-------|-----------
//	low CPU + good RT |    0.5     |      0.25      |  0.75 |   67%
//	mid CPU + good RT |    1.0     |      0.25      |  1.25 |   80%
//	high CPU + good RT|    2.0     |      0.25      |  2.25 |   89%
//	low CPU + bad RT  |    0.5     |      1.00      |  1.50 |   33%
//	mid CPU + bad RT  |    1.0     |      1.00      |  2.00 |   50%
//	high CPU + bad RT |    2.0     |      1.00      |  3.00 |   67%
//
// TODO: Consider making V configurable or tuning it based on observed
// latency sensitivity in production. Raising V to 1.0 would bring
// the CPU-weight range down to ~50-67% across scenarios.
const defaultV = 0.5

// Temperature for softmax distribution (higher = more uniform)
const softmaxTemp = 10.0

// ============== Latency Threshold Configuration ==============

// Default latency thresholds for intra-continental routing
const (
	DefaultLatencyGood    = 50.0
	DefaultLatencyNormal  = 100.0
	DefaultLatencyWarning = 150.0
)

// Inter-continental latency thresholds (higher due to longer distances)
const (
	InterContLatencyGood    = 100.0
	InterContLatencyNormal  = 150.0
	InterContLatencyWarning = 200.0
)

// High-latency intercontinental thresholds (e.g., US West to Asia)
const (
	HighLatencyGood    = 150.0
	HighLatencyNormal  = 200.0
	HighLatencyWarning = 250.0
)

// Default thresholds for unconfigured continent pairs
const (
	DefaultInterContGood    = 100.0
	DefaultInterContNormal  = 150.0
	DefaultInterContWarning = 200.0

	DefaultIntraContGood    = 50.0
	DefaultIntraContNormal  = 100.0
	DefaultIntraContWarning = 150.0
)

// ============== Geographic Configuration ==============

// latencyConfig holds latency thresholds for different geographic scenarios
type latencyConfig struct {
	good    float64
	normal  float64
	warning float64
}

// ContinentPairConfigs maps continent pairs to custom latency thresholds
// Only pairs that need non-default values should be configured here
// All other inter-continental pairs will use DefaultInterCont thresholds
var ContinentPairConfigs = map[string]latencyConfig{
	// Asia-NorthAmerica custom thresholds
	"Asia-NorthAmerica": {good: 100, normal: 150, warning: 200},
	"NorthAmerica-Asia": {good: 100, normal: 150, warning: 200},
}

// ============== Dynamic CPU Thresholds by Core Count ==============

// CPUThresholds holds CPU thresholds for different core counts
type CPUThresholds struct {
	Low          float64 // Low CPU threshold for penalty calculation
	Mid          float64 // Medium CPU threshold
	High         float64 // High CPU threshold
	HysteresisUp float64 // Threshold to enter penalty state
	HysteresisDn float64 // Threshold to exit penalty state
}

// CPUConfig maps core count to CPU thresholds
var CPUConfig = map[int]CPUThresholds{
	2: {
		Low:          40.0, // CPULow for 2-core
		Mid:          60.0, // CPUMid for 2-core
		High:         80.0, // CPUHigh for 2-core
		HysteresisUp: 60.0, // Enter penalty at CPUMid
		HysteresisDn: 40.0, // Exit penalty at CPULow
	},
	4: {
		Low:          60.0,  // CPULow for 4-core
		Mid:          80.0,  // CPUMid for 4-core
		High:         100.0, // CPUHigh for 4-core
		HysteresisUp: 80.0,  // Enter penalty at CPUMid
		HysteresisDn: 60.0,  // Exit penalty at CPULow
	},
}

// GetCPUThresholds returns CPU thresholds for the given core count
func GetCPUThresholds(logicalCores int) CPUThresholds {
	if config, exists := CPUConfig[logicalCores]; exists {
		return config
	}
	// Default to 2-core thresholds for unknown core counts
	return CPUConfig[2]
}
