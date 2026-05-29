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
// Target range: both ~0.5 to ~3 across typical operating conditions.
// CPU and delay penalties are matched point-to-point (e.g. 80%CPU ≈ 80ms delay).
//
// 2-core (Mid=60%, α=2.0):  20%→0.26,  40%→0.51,  60%→1.0,  80%→1.95,  100%→3.79
// 4-core (Mid=80%, α=3.0):  40%→0.22,  60%→0.47,  80%→1.0,  100%→2.12,  110%→3.22
// Delay (good=20ms, β=0.20): 10ms→0.90, 20ms→1.0, 40ms→1.22, 60ms→1.49, 80ms→1.82
const (
	sensitivityCPU   = 2.0
	sensitivityDelay = 0.20
)

// Default weight for delay penalty in Lyapunov formula.
//
// Score = 1.0 * cpuPenalty + V * delayPenalty
// Both penalties have aligned value ranges for direct addition:
//
//	cpuPenalty  = exp(2.0 * (Qk/Mid  - 1))
//	delayPenalty = exp(0.20 * (rt/good - 1))
//
// With V=1.0, CPU and delay contribute equally across their ranges:
//
//	Scenario            | cpuPenalty | delayPenalty | Score  | CPU weight
//	--------------------|------------|--------------|--------|-----------
//	low CPU + good RT   |   0.51     |     1.00     |  1.51  |   34%
//	mid CPU + good RT   |   1.00     |     1.00     |  2.00  |   50%
//	high CPU + good RT  |   1.95     |     1.00     |  2.95  |   66%
//	low CPU + bad RT    |   0.51     |     1.49     |  2.00  |   26%
//	mid CPU + bad RT    |   1.00     |     1.49     |  2.49  |   40%
//	high CPU + bad RT   |   1.95     |     1.49     |  3.44  |   57%
//
// Point-to-point match (2-core): 80%CPU (1.95) ≈ 80ms delay (1.82).
// Point-to-point match (4-core): 100%CPU (2.12) ≈ 80ms delay (1.82).
// V=1.0 gives true 50:50 weighting when both dimensions are at their baselines.
const defaultV = 1.0

// Temperature for softmax distribution (higher = more uniform).
// Matched to Score range ~1.5~5: T=2 gives good differentiation without starving the worst node.
//
// Real-world example with 3 candidate nodes (Mid=60%, good=20ms, α=2.0, β=0.20, V=1.0):
//
//	Node              | Qk+Δ  | Delay  | cpuPenalty | delayPenalty | Score
//	------------------|-------|--------|------------|--------------|------
//	139.59.119.32     | 45.1% |  35ms  |   0.612    |    1.105     | 1.717
//	34.146.177.77     | 39.8% | 49.5ms |   0.514    |    1.238     | 1.752
//	158.247.238.238   | 25.9% | 57.5ms |   0.327    |    1.297     | 1.624
//
// Softmax with T=2: P = [34.0%, 32.7%, 33.3%]
// Best node gets ~39% traffic, worst ~28% — clear preference without starving anyone.
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

// CPUThresholds holds CPU thresholds and sensitivity for different core counts.
// 4-core machines have higher baseline (Mid=80%) and narrower operating range,
// so they use a higher sensitivityCPU to produce penalties comparable to 2-core.
//
// Alignment targets (2-core α=2.0 vs 4-core α=3.0):
//   2-core: 40%→0.51, 60%→1.0, 80%→1.95, 100%→3.79
//   4-core: 60%→0.47, 80%→1.0, 100%→2.12, 110%→3.22
type CPUThresholds struct {
	Low           float64 // Low CPU threshold for penalty calculation
	Mid           float64 // Medium CPU threshold
	High          float64 // High CPU threshold
	SensitivityCPU float64 // α for CPU penalty (default: sensitivityCPU const, overridden per core count)
	HysteresisUp  float64 // Threshold to enter penalty state
	HysteresisDn  float64 // Threshold to exit penalty state
}

// CPUConfig maps core count to CPU thresholds
var CPUConfig = map[int]CPUThresholds{
	2: {
		Low:           40.0, // CPULow for 2-core
		Mid:           60.0, // CPUMid for 2-core
		High:          80.0, // CPUHigh for 2-core
		SensitivityCPU: 2.0, // α for 2-core: 40%→0.51, 60%→1.0, 100%→3.79
		HysteresisUp:  60.0, // Enter penalty at CPUMid
		HysteresisDn:  40.0, // Exit penalty at CPULow
	},
	4: {
		Low:           60.0, // CPULow for 4-core
		Mid:           80.0, // CPUMid for 4-core
		High:          100.0, // CPUHigh for 4-core
		SensitivityCPU: 3.0, // α for 4-core: 60%→0.47, 80%→1.0, 100%→2.12
		HysteresisUp:  80.0, // Enter penalty at CPUMid
		HysteresisDn:  60.0, // Exit penalty at CPULow
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
