package edge_domain

// ============== CPU Threshold Configuration ==============

// Default CPU thresholds for 2-core machines
const (
	CPULow  = 40.0
	CPUMid  = 60.0
	CPUHigh = 80.0
)

// Sensitivity factors for exponential penalty functions.
// Higher values = steeper penalty curve.
//
//	cpuPenalty  = exp(sensitivityCPU  * (Qk/Mid  - 1))
//	delayPenalty = exp(sensitivityDelay * (rt/good - 1))
//
// Design: Both penalties MUST have the same value range so they can be directly added
// with V=1.0. CPU ratio range (0.33~1.67) is narrower than delay ratio range (0.5~4.0),
// so CPU uses a higher sensitivity to compensate.
//
// Target range: both ~0.5 to ~5 across typical operating conditions.
//
// CPU (Mid=60%, α=1.7):  20%→0.32,  40%→0.57,  60%→1.0,  80%→1.76,  100%→3.11
// Delay (good=20ms, β=0.55): 10ms→0.76, 20ms→1.0, 35ms→1.51, 60ms→3.00, 80ms→5.21
const (
	sensitivityCPU   = 1.7
	sensitivityDelay = 0.55
)

// Default weight for delay penalty in Lyapunov formula.
//
// Score = 1.0 * cpuPenalty + V * delayPenalty
// Both penalties have the SAME value range (~0.5 to ~5) for direct addition:
//
//	cpuPenalty  = exp(1.7 * (Qk/Mid  - 1))
//	delayPenalty = exp(0.55 * (rt/good - 1))
//
// With V=1.0, CPU and delay contribute equally across their ranges:
//
//	Scenario            | cpuPenalty | delayPenalty | Score  | CPU weight
//	--------------------|------------|--------------|--------|-----------
//	low CPU + good RT   |   0.57     |     1.00     |  1.57  |   36%
//	mid CPU + good RT   |   1.00     |     1.00     |  2.00  |   50%
//	high CPU + good RT  |   1.76     |     1.00     |  2.76  |   64%
//	low CPU + bad RT    |   0.57     |     3.00     |  3.57  |   16%
//	mid CPU + bad RT    |   1.00     |     3.00     |  4.00  |   25%
//	high CPU + bad RT   |   1.76     |     3.00     |  4.76  |   37%
//
// With aligned value ranges, V=1.0 means CPU and delay are truly equal-weighted.
// In practice, bad delay (60ms) gives penalty 3.0 — same ballpark as high CPU (80%) at 1.76.
// The worse dimension naturally dominates, which is the desired behavior.
const defaultV = 1.0

// Temperature for softmax distribution (higher = more uniform).
// Matched to Score range ~1.5~5: T=2 gives good differentiation without starving the worst node.
//
// Real-world example with 3 candidate nodes (Mid=60%, good=20ms, α=1.7, β=0.55, V=1.0):
//
//	Node              | Qk+Δ  | Delay  | cpuPenalty | delayPenalty | Score
//	------------------|-------|--------|------------|--------------|------
//	139.59.119.32     | 45.1% |  35ms  |   0.656    |    1.511     | 2.167
//	34.146.177.77     | 39.8% | 49.5ms |   0.564    |    2.250     | 2.814
//	158.247.238.238   | 25.9% | 57.5ms |   0.381    |    2.804     | 3.185
//
// Softmax with T=2: P = [43.0%, 31.1%, 25.9%]
// Best node gets ~43% traffic, worst ~26% — clear preference without starving anyone.
const softmaxTemp = 2.0

// ============== Latency Threshold Configuration ==============

// Default latency thresholds for intra-continental routing
const (
	DefaultLatencyGood    = 20.0
	DefaultLatencyNormal  = 40.0
	DefaultLatencyWarning = 60.0
)

// Inter-continental latency thresholds (higher due to longer distances)
const (
	InterContLatencyGood    = 60.0
	InterContLatencyNormal  = 100.0
	InterContLatencyWarning = 150.0
)

// High-latency intercontinental thresholds (e.g., US West to Asia)
const (
	HighLatencyGood    = 100.0
	HighLatencyNormal  = 150.0
	HighLatencyWarning = 200.0
)

// Default thresholds for unconfigured continent pairs
const (
	DefaultInterContGood    = 60.0
	DefaultInterContNormal  = 100.0
	DefaultInterContWarning = 150.0

	DefaultIntraContGood    = 20.0
	DefaultIntraContNormal  = 40.0
	DefaultIntraContWarning = 60.0
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
	"Asia-NorthAmerica": {good: 60, normal: 100, warning: 150},
	"NorthAmerica-Asia": {good: 60, normal: 100, warning: 150},
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
