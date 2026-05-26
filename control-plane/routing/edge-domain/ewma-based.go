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
		lambda:   0.7, // Weight for CPU (higher means more CPU-aware)
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
		if node.Continent == continent {
			nodeIps = append(nodeIps, node.PublicIP)
		}
	}

	// Fallback to local node if no continent match
	if len(nodeIps) <= 0 {
		logger.Warn("no available nodes in continent", slog.String("pre", pre), slog.String("continent", continent))
		nodeIps = []string{util.Config_.Node.IP.Public}
	}

	logger.Info("EWMA collected nodes", slog.String("pre", pre), slog.Int("nodeCount", len(nodeIps)))

	type nodeScore struct {
		nodeIp   string
		cpuEwma  float64
		latEwma  float64
		combined float64
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

		// Calculate combined score: λ * CPU_EWMA + (1-λ) * Latency_EWMA
		// Normalize latency to 0-100 scale (assuming latency in ms, divide by some factor)
		normLatEwma := latEwma / 10 // Convert ms to approximate 0-100 scale
		if normLatEwma > 100 {
			normLatEwma = 100
		}

		combined := r.lambda*cpuEwma + (1-r.lambda)*normLatEwma

		candidates = append(candidates, nodeScore{
			nodeIp:   nodeIp,
			cpuEwma:  cpuEwma,
			latEwma:  latEwma,
			combined: combined,
		})

		logger.Debug("EWMA node score",
			slog.String("pre", pre),
			slog.String("nodeIp", nodeIp),
			slog.Float64("cpuEwma", cpuEwma),
			slog.Float64("latEwma", latEwma),
			slog.Float64("normLatEwma", normLatEwma),
			slog.Float64("combinedScore", combined))
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

	// Convert to PathInfo
	var paths []routing.PathInfo
	for _, c := range candidates {
		paths = append(paths, routing.PathInfo{
			Hops: []string{c.nodeIp},
			Rtt:  c.combined,
		})
	}

	logger.Info("EWMA routing completed",
		slog.String("pre", pre),
		slog.Int("candidateCount", len(paths)),
		slog.Any("paths", paths))

	return paths, nil
}

// GetNodeRT returns the latency stats for a node (same as LatencyOnlyRouter)
func (r *EWMARouter) GetNodeRT(source routing.EndPoint, nodeIP string, pre string, logger *slog.Logger) *rece.LastCongestion {
	userContinent := source.Continent
	userCountry := source.Country
	userCity := source.City

	node, nodeExists := r.nodeTel[nodeIP]
	if !nodeExists {
		logger.Warn("node not found in telemetry", slog.String("pre", pre), slog.String("nodeIp", nodeIP))
		return nil
	}

	// Try different key formats
	keys := []string{
		userCity + "-" + nodeIP,
		userCity + "-" + node.City,
		userCountry + "-" + node.Country,
		userContinent + "-" + node.Continent,
		userContinent + "-general",
	}

	for _, key := range keys {
		congestion, exists := r.edgeAgg[key]
		if exists {
			return congestion
		}
	}

	return &rece.LastCongestion{}
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
