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
	"control-plane/routing/routing"
)

// omEdgeRisk calculates edge weight based on CPU pressure, loss, and latency
func omEdgeRisk(cpuPressure, loss, latency float64) float64 {
	const (
		CPULow   = 40.0
		CPUMid   = 60.0
		CPUHigh  = 80.0
		cpuPower = 2.0

		lossInflection = 0.05
		lossSharpness  = 40.0
		latencyMax     = 50.0
		latPower       = 1.5
		wCPU           = 0.5
		wLoss          = 0.0
		wLat           = 0.5
	)

	var cpuRisk float64
	if cpuPressure < CPUMid {
		cpuRisk = 0.0
	} else if cpuPressure >= CPUHigh {
		cpuRisk = 1.0
	} else {
		cpuRatio := (cpuPressure - CPUMid) / (CPUHigh - CPUMid)
		cpuRisk = math.Pow(cpuRatio, cpuPower)
	}

	var lossRisk float64
	if loss >= 1.0 {
		lossRisk = 1.0
	} else if loss <= 0 {
		lossRisk = 0
	} else {
		lossRisk = 1.0 / (1.0 + math.Exp(-lossSharpness*(loss-lossInflection)))
	}

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

func omParseCost266Edges(filePath string) []*graph.Edge {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("Failed to open file: %v\n", err)
		return nil
	}
	defer file.Close()

	var edges []*graph.Edge
	var inLinks bool

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
						rttStr := strings.TrimSpace(line[idx+6:])
						if len(rttStr) > 2 && rttStr[len(rttStr)-2:] == "ms" {
							fmt.Sscanf(rttStr[:len(rttStr)-2], "%f", &rawRTT)
						}
					}

					edgeWeight := omEdgeRisk(defaultCPU, defaultLoss, rawRTT)

					edges = append(edges, &graph.Edge{
						SourceIp:      source,
						DestinationIp: target,
						EdgeWeight:    edgeWeight,
						Latency:       rawRTT,
						Loss:          defaultLoss,
					})
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

func omCalculateRawRTT(hops []string, edges []*graph.Edge) float64 {
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

func TestONEWANMultiSolver(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cost266File := "evaluation/cost266"
	edges := omParseCost266Edges(cost266File)
	if edges == nil || len(edges) == 0 {
		t.Fatal("Failed to parse cost266 topology")
	}

	solver := NewONEWANSolver(edges, 2)

	source := "Amsterdam"
	dests := []string{"Berlin", "Paris"} // 只测试2个end

	fmt.Printf("\n========================================\n")
	fmt.Printf("ONEWAN Multi Solver Test: %s -> %v\n", source, dests)
	fmt.Printf("========================================\n\n")

	// Build graph for ComputingMulti
	graph_ := make(map[string][]*graph.Edge)
	for _, e := range edges {
		graph_[e.SourceIp] = append(graph_[e.SourceIp], e)
	}

	// Step 1: Show individual paths for each destination
	fmt.Printf("[STEP 1] Individual Paths for Each Destination (Candidate Paths):\n")
	fmt.Printf("===============================================================\n")

	type destCandidates struct {
		end   string
		paths []routing.PathInfo
	}
	var allCandidates []destCandidates

	for _, dest := range dests {
		fmt.Printf("\n  Destination: %s\n", dest)
		fmt.Printf("  %s\n", strings.Repeat("-", 60))

		paths, err := solver.Computing(source, dest, "TEST", logger)
		if err != nil {
			fmt.Printf("    Error: %v\n", err)
			continue
		}

		for i, path := range paths {
			hopStr := strings.Join(path.Hops, " -> ")
			fmt.Printf("    [%d] Weight=%.4f, RawRTT=%.2fms, Hops=%d: %s\n",
				i+1, path.Rtt, path.RawRTT, len(path.Hops)-1, hopStr)
		}

		if len(paths) > 0 {
			allCandidates = append(allCandidates, destCandidates{end: dest, paths: paths})
		}
	}

	// Step 2: Use ComputingMulti for multiple destinations (Beam Search)
	fmt.Printf("\n[STEP 2] ComputingMulti Results (Beam Search Selection):\n")
	fmt.Printf("==========================================================\n")

	paths, err := ComputingMulti(solver, source, dests, "TEST", logger)
	if err != nil {
		t.Fatalf("Error finding paths: %v", err)
	}

	fmt.Printf("\nSelected Paths (Global Optimization):\n")
	fmt.Printf("%s\n", strings.Repeat("-", 80))
	for i, path := range paths {
		hopStr := strings.Join(path.Hops, " -> ")
		fmt.Printf("  [%d] Weight=%.4f, RawRTT=%.2fms, Hops=%d: %s\n",
			i+1, path.Rtt, path.RawRTT, len(path.Hops)-1, hopStr)
	}
	fmt.Printf("\n  Total paths selected: %d\n", len(paths))
	fmt.Printf("  Expected: %d (2 paths per destination × %d destinations)\n", 2*len(dests), len(dests))

	// Step 3: Show node load distribution
	fmt.Printf("\n[STEP 3] Node Load Distribution:\n")
	fmt.Printf("=================================\n")
	nodeLoad := make(map[string]int)
	for _, path := range paths {
		for _, node := range path.Hops {
			nodeLoad[node]++
		}
	}

	// Sort nodes by load (descending)
	type nodeLoadInfo struct {
		node string
		load int
	}
	var sortedLoad []nodeLoadInfo
	for node, load := range nodeLoad {
		sortedLoad = append(sortedLoad, nodeLoadInfo{node: node, load: load})
	}
	for i := 0; i < len(sortedLoad)-1; i++ {
		for j := i + 1; j < len(sortedLoad); j++ {
			if sortedLoad[j].load > sortedLoad[i].load {
				sortedLoad[i], sortedLoad[j] = sortedLoad[j], sortedLoad[i]
			}
		}
	}

	fmt.Printf("  %-20s %s\n", "Node", "Load")
	fmt.Printf("  %s\n", strings.Repeat("-", 30))
	for _, nl := range sortedLoad {
		fmt.Printf("  %-20s %d\n", nl.node, nl.load)
	}
	fmt.Printf("\n")
}

func TestONEWANMultiSolverMultiplePairs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cost266File := "evaluation/cost266"
	edges := omParseCost266Edges(cost266File)
	if edges == nil || len(edges) == 0 {
		t.Fatal("Failed to parse cost266 topology")
	}

	solver := NewONEWANSolver(edges, 2)

	testCases := []struct {
		start string
		ends  []string
	}{
		{"Amsterdam", []string{"Berlin", "Paris", "Frankfurt"}},
		{"London", []string{"Paris", "Brussels"}},
		{"Frankfurt", []string{"Munich", "Berlin", "Hamburg"}},
		{"Brussels", []string{"Amsterdam", "Paris", "London"}},
	}

	fmt.Println("\n=== ONEWAN Multi Solver - Multiple Test Cases ===")

	for _, tc := range testCases {
		fmt.Printf("\n========================================\n")
		fmt.Printf("Test: %s -> %v\n", tc.start, tc.ends)
		fmt.Printf("========================================\n")

		paths, err := ComputingMulti(solver, tc.start, tc.ends, "TEST", logger)
		if err != nil {
			fmt.Printf("Error: %v\n\n", err)
			continue
		}

		fmt.Printf("[Results - Round-Robin Interleaved]:\n")
		for i, path := range paths {
			hopStr := strings.Join(path.Hops, " -> ")
			fmt.Printf("  [%d] Weight=%.4f, RawRTT=%.2fms, Hops=%d: %s\n",
				i+1, path.Rtt, path.RawRTT, len(path.Hops)-1, hopStr)
		}
		fmt.Printf("  Total paths: %d (expected: %d)\n\n", len(paths), 2*len(tc.ends))
	}
}

func TestONEWANMultiSolverSingleDestination(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cost266File := "evaluation/cost266"
	edges := omParseCost266Edges(cost266File)
	if edges == nil || len(edges) == 0 {
		t.Fatal("Failed to parse cost266 topology")
	}

	solver := NewONEWANSolver(edges, 2)

	source := "Amsterdam"
	dests := []string{"Berlin"}

	fmt.Printf("\n========================================\n")
	fmt.Printf("ONEWAN Multi Solver Test (Single Destination): %s -> %v\n", source, dests)
	fmt.Printf("========================================\n\n")

	paths, err := ComputingMulti(solver, source, dests, "TEST", logger)
	if err != nil {
		t.Fatalf("Error finding paths: %v", err)
	}

	fmt.Printf("[Results]:\n")
	for i, path := range paths {
		hopStr := strings.Join(path.Hops, " -> ")
		fmt.Printf("  [%d] Weight=%.4f, RawRTT=%.2fms, Hops=%d: %s\n",
			i+1, path.Rtt, path.RawRTT, len(path.Hops)-1, hopStr)
	}
	fmt.Printf("  Total paths: %d\n\n", len(paths))
}

func TestONEWANMultiSolverEmptyDestinations(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cost266File := "evaluation/cost266"
	edges := omParseCost266Edges(cost266File)
	if edges == nil || len(edges) == 0 {
		t.Fatal("Failed to parse cost266 topology")
	}

	solver := NewONEWANSolver(edges, 2)

	source := "Amsterdam"
	dests := []string{}

	fmt.Printf("\n========================================\n")
	fmt.Printf("ONEWAN Multi Solver Test (Empty Destinations): %s -> %v\n", source, dests)
	fmt.Printf("========================================\n\n")

	paths, err := ComputingMulti(solver, source, dests, "TEST", logger)
	if err != nil {
		t.Fatalf("Error finding paths: %v", err)
	}

	fmt.Printf("[Results]:\n")
	for i, path := range paths {
		hopStr := strings.Join(path.Hops, " -> ")
		fmt.Printf("  [%d] Weight=%.4f, RawRTT=%.2fms, Hops=%d: %s\n",
			i+1, path.Rtt, path.RawRTT, len(path.Hops)-1, hopStr)
	}
	fmt.Printf("  Total paths: %d (should fall back to regular Computing)\n\n", len(paths))
}
