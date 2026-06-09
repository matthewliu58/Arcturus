package core_domain

import (
	"bufio"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"testing"

	"control-plane/routing/graph"
)

// calculateRawRTT calculates the actual path latency based on Edge.Latency
func calculateRawRTT(hops []string, edges []*graph.Edge) float64 {
	rawRTT := 0.0
	for i := 0; i < len(hops)-1; i++ {
		source := hops[i]
		target := hops[i+1]
		for _, edge := range edges {
			if edge.SourceIp == source && edge.DestinationIp == target {
				rawRTT += edge.Latency
				break
			}
		}
	}
	return rawRTT
}

func TestSinglePathFinding(t *testing.T) {

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cost266File := "evaluation/cost266"
	edges := parseCost266Edges(cost266File)
	if edges == nil || len(edges) == 0 {
		t.Fatal("Failed to parse cost266 topology")
	}

	solver := NewKShortestSolver(edges, 5)

	// Test single pair: Amsterdam -> Berlin
	source := "Amsterdam"
	dest := "Berlin"

	fmt.Printf("\n========================================\n")
	fmt.Printf("Test: %s -> %s\n", source, dest)
	fmt.Printf("========================================\n\n")

	// Get all K=5 paths
	allPaths := findAllKShortestPaths(solver, source, dest, logger)

	fmt.Printf("[STEP 1] K=5 Shortest Paths:\n")
	for i, path := range allPaths {
		hopStr := strings.Join(path.hops, " -> ")
		rawRTT := calculateRawRTT(path.hops, edges)
		fmt.Printf("  [%d] Weight=%.4f, RawRTT=%.2fms, Hops=%d: %s\n",
			i+1, path.cost, rawRTT, len(path.hops)-1, hopStr)
	}

	// Show selection process
	fmt.Printf("\n[STEP 2] Path Classification:\n")
	shortPaths, longPaths := classifyPaths(allPaths)
	fmt.Printf("  Short Paths (<=4 hops): %d paths\n", len(shortPaths))
	for i, p := range shortPaths {
		hopStr := strings.Join(p.hops, " -> ")
		rawRTT := calculateRawRTT(p.hops, edges)
		fmt.Printf("    [%d] Weight=%.4f, RawRTT=%.2fms, %d hops: %s\n", i+1, p.cost, rawRTT, len(p.hops)-1, hopStr)
	}
	fmt.Printf("  Long Paths (>4 hops): %d paths\n", len(longPaths))
	for i, p := range longPaths {
		hopStr := strings.Join(p.hops, " -> ")
		rawRTT := calculateRawRTT(p.hops, edges)
		fmt.Printf("    [%d] Weight=%.4f, RawRTT=%.2fms, %d hops: %s\n", i+1, p.cost, rawRTT, len(p.hops)-1, hopStr)
	}

	// Select top 2
	fmt.Printf("\n[STEP 3] Select Top 2 Paths:\n")
	selected := selectTopPaths(allPaths, 2)
	for i, p := range selected {
		hopStr := strings.Join(p.hops, " -> ")
		rawRTT := calculateRawRTT(p.hops, edges)
		fmt.Printf("  [%d] Weight=%.4f, RawRTT=%.2fms, Hops=%d: %s\n",
			i+1, p.cost, rawRTT, len(p.hops)-1, hopStr)
	}
	fmt.Printf("\n")
}

// EdgeRisk calculates edge weight based on CPU pressure, loss, and latency
// This is a simplified version for testing purposes
// Reference: lyapunov-config.go
func EdgeRisk(cpuPressure, loss, latency float64) float64 {
	const (
		// CPU thresholds (referenced from lyapunov-config.go)
		// CPU < 60: no penalty
		// 60 <= CPU < 80: increasing penalty
		// CPU >= 80: max penalty
		CPULow   = 40.0 // Hysteresis down threshold
		CPUMid   = 60.0 // Threshold to start penalty
		CPUHigh  = 80.0 // Threshold for max penalty
		cpuPower = 2.0  // Power for penalty curve

		lossInflection = 0.05
		lossSharpness  = 40.0
		latencyMax     = 50.0
		latPower       = 1.5
		wCPU           = 0.5
		wLoss          = 0.0
		wLat           = 0.5
	)

	// CPU risk: no penalty below CPUMid (60), penalty increases from 60 to 80, max at 80+
	var cpuRisk float64
	if cpuPressure < CPUMid {
		// CPU < 60: no penalty
		cpuRisk = 0.0
	} else if cpuPressure >= CPUHigh {
		// CPU >= 80: max penalty
		cpuRisk = 1.0
	} else {
		// 60 <= CPU < 80: interpolate penalty
		cpuRatio := (cpuPressure - CPUMid) / (CPUHigh - CPUMid)
		cpuRisk = math.Pow(cpuRatio, cpuPower)
	}

	// Loss risk: sigmoid
	var lossRisk float64
	if loss >= 1.0 {
		lossRisk = 1.0
	} else if loss <= 0 {
		lossRisk = 0
	} else {
		lossRisk = 1.0 / (1.0 + math.Exp(-lossSharpness*(loss-lossInflection)))
	}

	// Latency risk: power curve
	latRatio := latency / latencyMax
	if latRatio > 1.0 {
		latRatio = 1.0
	}
	if latRatio < 0 {
		latRatio = 0
	}
	latRisk := math.Pow(latRatio, latPower)

	return wCPU*cpuRisk + wLoss*lossRisk + wLat*latRisk
}

func parseCost266Edges(filePath string) []*graph.Edge {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("Failed to open file: %v\n", err)
		return nil
	}
	defer file.Close()

	var edges []*graph.Edge
	var inLinks bool

	// Default values as specified by user
	defaultCPU := 30.0
	defaultLoss := 0.0

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "LINKS") {
			inLinks = true
			continue
		}
		if inLinks && strings.TrimSpace(line) == ")" {
			break
		}
		if inLinks && line != "" && !strings.HasPrefix(line, "#") {
			start := strings.Index(line, "(")
			end := strings.Index(line, ")")
			if start != -1 && end != -1 {
				nodes := strings.Fields(line[start+1 : end])
				if len(nodes) >= 2 {
					source := nodes[0]
					target := nodes[1]

					var rawRTT float64 = 1.0
					if idx := strings.Index(line, "# RTT:"); idx != -1 {
						// "# RTT:" has 6 characters
						rttStr := strings.TrimSpace(line[idx+6:])
						if len(rttStr) > 2 && rttStr[len(rttStr)-2:] == "ms" {
							fmt.Sscanf(rttStr[:len(rttStr)-2], "%f", &rawRTT)
						}
					}

					// Calculate edge weight using EdgeRisk function
					edgeWeight := EdgeRisk(defaultCPU, defaultLoss, rawRTT)

					edges = append(edges, &graph.Edge{
						SourceIp:      source,
						DestinationIp: target,
						EdgeWeight:    edgeWeight,
						Latency:       rawRTT,
						Loss:          defaultLoss,
					})
					// Add reverse edge for bidirectional
					edges = append(edges, &graph.Edge{
						SourceIp:      target,
						DestinationIp: source,
						EdgeWeight:    edgeWeight,
						Latency:       rawRTT,
						Loss:          defaultLoss,
					})
				}
			}
		}
	}
	return edges
}

func findAllKShortestPaths(solver *KShortestSolver, start, end string, logger *slog.Logger) []Path {
	graph_ := make(map[string][]*graph.Edge)
	for _, e := range solver.edges {
		graph_[e.SourceIp] = append(graph_[e.SourceIp], e)
	}

	var result []Path

	// Use the solver's yensAlgorithm instead of manual implementation
	// Build graph structure
	nodes := make(map[string]struct{})
	for _, e := range solver.edges {
		nodes[e.SourceIp] = struct{}{}
		nodes[e.DestinationIp] = struct{}{}
	}

	if _, ok := nodes[start]; !ok {
		fmt.Printf("Start node %s not found\n", start)
		return result
	}
	if _, ok := nodes[end]; !ok {
		fmt.Printf("End node %s not found\n", end)
		return result
	}

	paths, err := solver.yensAlgorithm(start, end, graph_, logger)
	if err != nil {
		fmt.Printf("Error finding paths: %v\n", err)
		return result
	}

	// Filter paths that actually reach the destination and have no duplicate nodes
	for _, path := range paths {
		if len(path.hops) > 0 && path.hops[len(path.hops)-1] == end && !hasDuplicateNodes(path.hops) {
			result = append(result, path)
		}
	}

	// Sort by cost and limit to k
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].cost < result[i].cost {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	if len(result) > solver.k {
		result = result[:solver.k]
	}

	return result
}

func hasDuplicateNodes(hops []string) bool {
	seen := make(map[string]bool)
	for _, hop := range hops {
		if seen[hop] {
			return true
		}
		seen[hop] = true
	}
	return false
}

func classifyPaths(paths []Path) ([]Path, []Path) {
	var short, long []Path
	for _, p := range paths {
		if len(p.hops)-1 <= 4 {
			short = append(short, p)
		} else {
			long = append(long, p)
		}
	}
	return short, long
}

func selectTopPaths(paths []Path, count int) []Path {
	short, long := classifyPaths(paths)

	// Sort short paths by RTT
	for i := 0; i < len(short)-1; i++ {
		for j := i + 1; j < len(short); j++ {
			if short[j].cost < short[i].cost {
				short[i], short[j] = short[j], short[i]
			}
		}
	}

	// Sort long paths by RTT
	for i := 0; i < len(long)-1; i++ {
		for j := i + 1; j < len(long); j++ {
			if long[j].cost < long[i].cost {
				long[i], long[j] = long[j], long[i]
			}
		}
	}

	var result []Path
	remaining := count

	for i := 0; i < len(short) && remaining > 0; i++ {
		result = append(result, short[i])
		remaining--
	}
	for i := 0; i < len(long) && remaining > 0; i++ {
		result = append(result, long[i])
		remaining--
	}

	return result
}
