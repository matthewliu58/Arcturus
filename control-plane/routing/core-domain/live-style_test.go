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

// lsEdgeRisk calculates edge weight based on CPU pressure, loss, and latency
func lsEdgeRisk(cpuPressure, loss, latency float64) float64 {
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

func lsParseCost266Edges(filePath string) []*graph.Edge {
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

					edgeWeight := lsEdgeRisk(defaultCPU, defaultLoss, rawRTT)

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

func lsCalculateRawRTT(hops []string, edges []*graph.Edge) float64 {
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

func TestLiveStyleSolver(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cost266File := "evaluation/cost266"
	edges := lsParseCost266Edges(cost266File)
	if edges == nil || len(edges) == 0 {
		t.Fatal("Failed to parse cost266 topology")
	}

	solver := NewLiveStyleSolver(edges, 2)

	source := "Amsterdam"
	dest := "Berlin"

	fmt.Printf("\n========================================\n")
	fmt.Printf("LiveStyle Solver Test: %s -> %s\n", source, dest)
	fmt.Printf("========================================\n\n")

	// Build graph for yensAlgorithm
	graph_ := make(map[string][]*graph.Edge)
	for _, e := range edges {
		graph_[e.SourceIp] = append(graph_[e.SourceIp], e)
	}

	// Step 1: Get all K=5 paths using underlying solver
	kspSolver := NewKShortestSolver(edges, 5)
	allPaths, err := kspSolver.yensAlgorithm(source, dest, graph_, logger)
	if err != nil {
		t.Fatalf("Error finding paths: %v", err)
	}

	fmt.Printf("[STEP 1] K=5 Shortest Paths:\n")
	for i, path := range allPaths {
		hopStr := strings.Join(path.hops, " -> ")
		fmt.Printf("  [%d] Weight=%.4f, RawRTT=%.2fms, Hops=%d: %s\n",
			i+1, path.cost, path.rawRTT, len(path.hops)-1, hopStr)
	}

	// Step 2: Show classification process (filter >10 hops, sort by rawRTT)
	fmt.Printf("\n[STEP 2] Path Classification:\n")
	var validPaths, filteredPaths []Path
	for _, p := range allPaths {
		if len(p.hops)-1 <= 10 {
			validPaths = append(validPaths, p)
		} else {
			filteredPaths = append(filteredPaths, p)
		}
	}
	fmt.Printf("  Valid Paths (<=10 hops): %d paths\n", len(validPaths))
	for i, p := range validPaths {
		hopStr := strings.Join(p.hops, " -> ")
		fmt.Printf("    [%d] Weight=%.4f, RawRTT=%.2fms, %d hops: %s\n", i+1, p.cost, p.rawRTT, len(p.hops)-1, hopStr)
	}
	fmt.Printf("  Filtered Paths (>10 hops): %d paths\n", len(filteredPaths))
	for i, p := range filteredPaths {
		hopStr := strings.Join(p.hops, " -> ")
		fmt.Printf("    [%d] Weight=%.4f, RawRTT=%.2fms, %d hops: %s\n", i+1, p.cost, p.rawRTT, len(p.hops)-1, hopStr)
	}

	// Step 3: Select top 2 paths
	fmt.Printf("\n[STEP 3] Select Top 2 Paths:\n")
	selectedPaths := solver.selectTopPaths(allPaths, 2)
	for i, p := range selectedPaths {
		hopStr := strings.Join(p.hops, " -> ")
		fmt.Printf("  [%d] Weight=%.4f, RawRTT=%.2fms, Hops=%d: %s\n",
			i+1, p.cost, p.rawRTT, len(p.hops)-1, hopStr)
	}
	fmt.Printf("\n")
}

// func TestLiveStyleSolverMultiplePairs(t *testing.T) {
// 	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
// 		Level: slog.LevelInfo,
// 	}))

// 	cost266File := "evaluation/cost266"
// 	edges := lsParseCost266Edges(cost266File)
// 	if edges == nil || len(edges) == 0 {
// 		t.Fatal("Failed to parse cost266 topology")
// 	}

// 	solver := NewLiveStyleSolver(edges, 2)

// 	testCases := []struct {
// 		start string
// 		end   string
// 	}{
// 		{"Amsterdam", "Berlin"},
// 		{"London", "Paris"},
// 		{"Frankfurt", "Munich"},
// 		{"Brussels", "Amsterdam"},
// 		{"Paris", "Strasbourg"},
// 	}

// 	fmt.Println("\n=== LiveStyle Solver - Multiple Test Cases ===")

// 	for _, tc := range testCases {
// 		fmt.Printf("\n----------------------------------------\n")
// 		fmt.Printf("Test: %s -> %s\n", tc.start, tc.end)
// 		fmt.Printf("----------------------------------------\n")

// 		paths, err := solver.Computing(tc.start, tc.end, "TEST", logger)
// 		if err != nil {
// 			fmt.Printf("Error: %v\n", err)
// 			continue
// 		}

// 		for i, path := range paths {
// 			hopStr := strings.Join(path.Hops, " -> ")
// 			fmt.Printf("  [%d] Weight=%.4f, RawRTT=%.2fms, Hops=%d: %s\n",
// 				i+1, path.Rtt, path.RawRTT, len(path.Hops)-1, hopStr)
// 		}
// 	}
// }
