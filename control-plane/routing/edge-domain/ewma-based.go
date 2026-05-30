package edge_domain

import (
	agg "control-plane/aggregator"
	rece "control-plane/receive-info"
	"control-plane/routing/routing"
	"control-plane/util"
	"log/slog"
	"sync"
)

// EWMA-based Router using Exponentially Weighted Moving Average
// Core idea: look at smoothed historical state instead of instantaneous values
// EWMA update formula: X(t) = α * x(t) + (1-α) * X(t-1)
//
// Characteristics:
//   - More stable, suppresses short-term noise
//   - Avoids frequent oscillation
//   - Slower response, may lag behind bursts
//   - History-aware smoothed control
type EWMARouter struct {
	nodeTel map[string]*agg.NodeTelemetry
	edgeAgg map[string]*rece.LastCongestion

	// EWMA state per node
	mu      sync.RWMutex
	cpuEWMA map[string]float64 // CPU EWMA
	latEWMA map[string]float64 // Latency EWMA

	// Configuration
	cpuAlpha float64 // CPU EWMA smoothing factor (0 < α <= 1)
	latAlpha float64 // Latency EWMA smoothing factor
	lambda   float64 // Weight for CPU in combined score (0 <= λ <= 1)
}

func NewEWMARouter(
	nodeTel map[string]*agg.NodeTelemetry,
	edgeAgg map[string]*rece.LastCongestion,
) *EWMARouter {
	return &EWMARouter{
		nodeTel:  nodeTel,
		edgeAgg:  edgeAgg,
		cpuEWMA:  make(map[string]float64),
		latEWMA:  make(map[string]float64),
		cpuAlpha: 0.3, // Smoothing factor for CPU
		latAlpha: 0.3, // Smoothing factor for latency
		lambda:   0.5, // Weight for CPU (1:1 ratio with latency)
	}
}

// SetAlpha allows configuring the smoothing factors
// alpha: smoothing factor (0 < α <= 1), higher = more weight on recent data
// lambda: CPU weight in combined score (0 <= λ <= 1)
func (r *EWMARouter) SetAlpha(alpha, lambda float64) {
	r.cpuAlpha = alpha
	r.latAlpha = alpha
	r.lambda = lambda
}

// updateEWMACPU updates the CPU EWMA for a node
// X(t) = α * x(t) + (1-α) * X(t-1)
func (r *EWMARouter) updateEWMACPU(nodeIP string, cpuUsage float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	current, exists := r.cpuEWMA[nodeIP]
	if !exists {
		// Initialize with first observation
		r.cpuEWMA[nodeIP] = cpuUsage
		return
	}

	// EWMA formula
	r.cpuEWMA[nodeIP] = r.cpuAlpha*cpuUsage + (1-r.cpuAlpha)*current
}

// updateEWMA latency updates the latency EWMA for a node
func (r *EWMARouter) updateEWMALatency(nodeIP string, latency float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	current, exists := r.latEWMA[nodeIP]
	if !exists {
		// Initialize with first observation
		r.latEWMA[nodeIP] = latency
		return
	}

	// EWMA formula
	r.latEWMA[nodeIP] = r.latAlpha*latency + (1-r.latAlpha)*current
}

// getCPUEWMA returns the CPU EWMA for a node
func (r *EWMARouter) getCPUEWMA(nodeIP string) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	val, exists := r.cpuEWMA[nodeIP]
	if !exists {
		return 100 // Default to max load if no history
	}
	return val
}

// getLatEWMA returns the latency EWMA for a node
func (r *EWMARouter) getLatEWMA(nodeIP string) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	val, exists := r.latEWMA[nodeIP]
	if !exists {
		return 500 // Default to high latency if no history
	}
	return val
}

// Computing selects nodes using EWMA-based scoring
// Score = λ * CPU_EWMA + (1-λ) * Latency_EWMA
// Choose node with minimum score
func (r *EWMARouter) Computing(endPoints routing.EndPoints, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	logger.Info("EWMA last-mile routing start", slog.String("pre", pre), slog.Any("endPoints", endPoints))

	source := endPoints.Source
	continent := source.Continent

	// Collect all nodes in the same continent
	var nodeIps []string
	for _, node := range r.nodeTel {
		//if node.Continent == continent {
		nodeIps = append(nodeIps, node.PublicIP)
		//}
	}

	// Fallback to local node if no continent match
	if len(nodeIps) <= 0 {
		logger.Warn("no available nodes in continent", slog.String("pre", pre), slog.String("continent", continent))
		nodeIps = []string{util.Config_.Node.IP.Public}
	}

	logger.Info("EWMA collected nodes", slog.String("pre", pre), slog.Int("nodeCount", len(nodeIps)))

	type nodeScore struct {
		nodeIp      string
		cpuUsage    float64
		latency     float64
		cpuEwma     float64
		latEwma     float64
		normLatEwma float64
		combined    float64
	}

	var candidates []nodeScore

	for _, nodeIp := range nodeIps {
		tel, telOk := r.nodeTel[nodeIp]
		if !telOk {
			logger.Warn("EWMA skip node: missing telemetry", slog.String("pre", pre), slog.String("nodeIp", nodeIp))
			continue
		}

		// Get current CPU
		cpuUsage := tel.Cpu.Usage
		if cpuUsage < 0 {
			cpuUsage = 100
		}

		// Update CPU EWMA
		r.updateEWMACPU(nodeIp, cpuUsage)
		cpuEwma := r.getCPUEWMA(nodeIp)

		// Get current latency
		stats := r.GetNodeRT(source, nodeIp, pre, logger)
		latency := 100.0 // Default latency
		if stats != nil && stats.Count > 0 {
			latency = stats.AvgRT
		}
		if latency <= 0 {
			latency = 100
		}

		// Update latency EWMA
		r.updateEWMALatency(nodeIp, latency)
		latEwma := r.getLatEWMA(nodeIp)

		// Normalize latency to 0-100 scale: latency_ms / 2 (e.g., 100ms -> 50, 20ms -> 10)
		// This makes 100ms latency equivalent to 50% CPU load (1:1 ratio)
		normLatEwma := latEwma / 2.0
		if normLatEwma > 100 {
			normLatEwma = 100
		}

		// Calculate combined score: λ * CPU_EWMA + (1-λ) * Latency_EWMA
		// With λ=0.5, CPU and latency have equal 1:1 weight
		combined := r.lambda*cpuEwma + (1-r.lambda)*normLatEwma

		candidates = append(candidates, nodeScore{
			nodeIp:      nodeIp,
			cpuUsage:    cpuUsage,
			latency:     latency,
			cpuEwma:     cpuEwma,
			latEwma:     latEwma,
			normLatEwma: normLatEwma,
			combined:    combined,
		})
	}

	if len(candidates) == 0 {
		logger.Warn("EWMA no valid nodes after scoring")
		return []routing.PathInfo{}, nil
	}

	// Sort by combined score (ascending - lower is better)
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].combined < candidates[i].combined {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Normalize: 1/combined as weight, then normalize to probabilities
	// Lower combined score → higher weight → higher traffic share
	totalWeight := 0.0
	rawWeights := make([]float64, len(candidates))
	for i, c := range candidates {
		if c.combined > 0 {
			rawWeights[i] = 1.0 / c.combined
		} else {
			rawWeights[i] = 1.0
		}
		totalWeight += rawWeights[i]
	}

	// Convert to PathInfo with normalized probabilities
	var paths []routing.PathInfo
	for i, c := range candidates {
		prob := rawWeights[i] / totalWeight
		paths = append(paths, routing.PathInfo{
			Hops:   []string{c.nodeIp},
			Rtt:    prob,
			RawRTT: c.combined,
		})
		logger.Info("EWMA node prob",
			slog.String("pre", pre),
			slog.String("nodeIp", c.nodeIp),
			slog.Float64("cpuUsage", c.cpuUsage),
			slog.Float64("cpuEwma", c.cpuEwma),
			slog.Float64("latency", c.latency),
			slog.Float64("latEwma", c.latEwma),
			slog.Float64("normLatEwma", c.normLatEwma),
			slog.Float64("combinedScore", c.combined),
			slog.Float64("prob", prob))
	}

	logger.Info("EWMA routing completed",
		slog.String("pre", pre),
		slog.Int("candidateCount", len(paths)),
		slog.Any("paths", paths))

	return paths, nil
}

// GetNodeRT returns the latency stats for a node (same as LatencyOnlyRouter)
func (r *EWMARouter) GetNodeRT(source routing.EndPoint, nodeIP string, pre string, logger *slog.Logger) *rece.LastCongestion {
	userCity := source.City

	_, nodeExists := r.nodeTel[nodeIP]
	if !nodeExists {
		logger.Warn("node not found in telemetry", slog.String("pre", pre), slog.String("nodeIp", nodeIP))
		return nil
	}

	key := userCity + "-" + nodeIP
	congestion, exists := r.edgeAgg[key]
	if exists {
		return congestion
	}

	// Default 50ms if no data
	logger.Warn("no latency data, using default 50ms", slog.String("pre", pre),
		slog.String("userCity", userCity), slog.String("nodeIP", nodeIP))
	return &rece.LastCongestion{Count: 1, AvgRT: 50.0}
}

// GetEWMASummary returns the current EWMA state for debugging
func (r *EWMARouter) GetEWMASummary() map[string]struct {
	CPU float64
	Lat float64
} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	summary := make(map[string]struct {
		CPU float64
		Lat float64
	})

	for nodeIP, cpuVal := range r.cpuEWMA {
		latVal := r.latEWMA[nodeIP]
		summary[nodeIP] = struct {
			CPU float64
			Lat float64
		}{CPU: cpuVal, Lat: latVal}
	}

	return summary
}
