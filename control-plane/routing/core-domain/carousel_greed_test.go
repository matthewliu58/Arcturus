package core_domain

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"control-plane/routing/graph"
	"control-plane/routing/routing"
)

// cgEdgeRisk calculates edge weight based on CPU pressure, loss, and latency
// Mirrors graph.EdgeRisk: no penalty below CPUMid (60), continuous penalty above with no cap.
func cgEdgeRisk(cpuPressure, loss, latency float64) float64 {
	const (
		// CPU thresholds: < 60 no penalty, ≥ 60 continuous penalty (no cap)
		CPUMid   = 60.0
		CPUHigh  = 80.0
		cpuPower = 2.0

		lossInflection = 0.05
		lossSharpness  = 40.0

		// Set to 20ms for cost266 dataset (90th percentile ≈ 19ms)
		latencyMax = 20.0
		latPower   = 1.5

		wCPU  = 0.5
		wLoss = 0.0
		wLat  = 0.5
	)

	// CPU risk: no penalty below CPUMid, continuous penalty above (no cap)
	var cpuRisk float64
	if cpuPressure < CPUMid {
		cpuRisk = 0.0
	} else {
		cpuRatio := (cpuPressure - CPUMid) / (CPUHigh - CPUMid)
		cpuRisk = math.Pow(cpuRatio, cpuPower)
	}

	// Loss risk: sigmoid, near-zero at 0% loss (currently unused: wLoss=0)
	var lossRisk float64
	if loss >= 1.0 {
		lossRisk = 1.0
	} else if loss <= 0 {
		lossRisk = 0
	} else {
		lossRisk = 1.0 / (1.0 + math.Exp(-lossSharpness*(loss-lossInflection)))
	}

	// Latency risk: continuous power curve (no cap)
	latRatio := latency / latencyMax
	if latRatio < 0 {
		latRatio = 0
	}
	latRisk := math.Pow(latRatio, latPower)

	return wCPU*cpuRisk + wLoss*lossRisk + wLat*latRisk
}

func cgParseCost266Edges(filePath string) []*graph.Edge {
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

					edgeWeight := cgEdgeRisk(defaultCPU, defaultLoss, rawRTT)

					edges = append(edges, &graph.Edge{
						SourceIp:      source,
						DestinationIp: target,
						EdgeWeight:    edgeWeight,
						Load:          defaultCPU,
						Latency:       rawRTT,
						Loss:          defaultLoss,
					})
					edges = append(edges, &graph.Edge{
						SourceIp:      target,
						DestinationIp: source,
						EdgeWeight:    edgeWeight,
						Load:          defaultCPU,
						Latency:       rawRTT,
						Loss:          defaultLoss,
					})
				}
			}
		}
	}
	return edges
}

func TestFlowOptimizationSolverMulti(t *testing.T) {
	// Create a logger that writes to both console and file
	logFile, err := os.Create("carousel_greed_test.log")
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}
	defer func() {
		logFile.Close()
	}()

	// Write to both console and file
	multiWriter := io.MultiWriter(os.Stdout, logFile)

	logger := slog.New(slog.NewTextHandler(multiWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Get the directory of the current test file
	_, filename, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(filename)
	cost266File := filepath.Join(testDir, "evaluation", "cost266")
	fmt.Printf("Topology file path: %s\n", cost266File)

	edges := cgParseCost266Edges(cost266File)
	if edges == nil || len(edges) == 0 {
		t.Fatal("Failed to parse cost266 topology")
	}

	fmt.Printf("\nTopology Statistics:\n")
	fmt.Printf("  Total edges: %d\n", len(edges))

	solver := NewFlowOptimizationSolver(edges)

	source := "Amsterdam"
	dests := []string{"Paris", "Berlin"} // 测试2个目标

	fmt.Printf("\n========================================\n")
	fmt.Printf("Flow Optimization Solver Test: %s -> %v\n", source, dests)
	fmt.Printf("========================================\n\n")

	// Step 1: Show individual paths for each destination using KSP with Latency
	fmt.Printf("\n[STEP 1] Individual Paths for Each Destination (Candidate Paths - using Latency):\n")
	fmt.Printf("================================================================================\n\n")

	type destCandidates struct {
		end   string
		paths []routing.PathInfo
	}
	var allCandidates []destCandidates

	for _, dest := range dests {
		fmt.Printf("\n")
		fmt.Printf("  ===================================================================\n")
		fmt.Printf("  DESTINATION: %s\n", dest)
		fmt.Printf("  ===================================================================\n")

		// Use KSP with pure Latency
		kspSolver := NewKShortestSolverWithLatency(edges, 5, true)
		pathResults, err := kspSolver.Computing(source, dest, "TEST", logger)
		if err != nil {
			fmt.Printf("    Error: %v\n", err)
			continue
		}

		fmt.Printf("    Total candidate paths found: %d\n", len(pathResults))
		fmt.Printf("    -------------------------------------------------------------------\n")
		fmt.Printf("      Idx | Latency  | Hops | Path\n")
		fmt.Printf("    -------------------------------------------------------------------\n")

		for i, path := range pathResults {
			hopStr := strings.Join(path.Hops, " -> ")
			if len(hopStr) > 55 {
				hopStr = hopStr[:52] + "..."
			}
			fmt.Printf("      [%d] | %-8s | %-4d | %s\n",
				i+1, fmt.Sprintf("%.2fms", path.Rtt), len(path.Hops)-1, hopStr)
		}
		fmt.Printf("    -------------------------------------------------------------------\n\n")

		// Detailed path information
		for i, path := range pathResults {
			fmt.Printf("      --- Path [%d] Details ------------------------------------------\n", i+1)
			fmt.Printf("      Full Path: %s\n", strings.Join(path.Hops, " -> "))
			fmt.Printf("      Latency: %.2fms, RawRTT: %.2fms, Hops: %d\n", path.Rtt, path.RawRTT, len(path.Hops)-1)
			fmt.Printf("      ----------------------------------------------------------------\n\n")
		}

		if len(pathResults) > 0 {
			allCandidates = append(allCandidates, destCandidates{end: dest, paths: pathResults})
		}
	}

	// Step 2: Use ComputingMulti for multiple destinations (Flow Optimization)
	fmt.Printf("\n[STEP 2] Flow Optimization Results (Multi-Destination):\n")
	fmt.Printf("======================================================\n")

	// First, compute thetaL for each destination (this is done inside ComputingMulti)
	fmt.Printf("\n[STEP 2.1] Theta_L Computation (Latency Constraints):\n")
	fmt.Printf("========================================================\n")
	for _, dest := range dests {
		// Compute thetaL by simulating the KSP computation
		kspSolver := NewKShortestSolverWithLatency(edges, 5, true)
		kspPaths, _ := kspSolver.Computing(source, dest, "thetaL_debug", logger)

		totalLat := 0.0
		fmt.Printf("\n  Destination: %s\n", dest)
		fmt.Printf("  Candidate paths for theta_L calculation:\n")
		fmt.Printf("  ---------------------------------------------------\n")
		for i, p := range kspPaths {
			totalLat += p.Rtt
			fmt.Printf("    [%d] Rtt=%.2fms, Hops=%d: %s\n", i+1, p.Rtt, len(p.Hops)-1, strings.Join(p.Hops, " -> "))
		}
		fmt.Printf("  ---------------------------------------------------\n")
		avgLat := totalLat / float64(len(kspPaths))
		thetaL := avgLat * 2.0
		fmt.Printf("  Total Rtt: %.2fms, Avg: %.2fms, Theta_L (=Avg*2): %.2fms\n", totalLat, avgLat, thetaL)
		fmt.Printf("  (Paths with Rtt <= %.2fms satisfy the latency constraint)\n", thetaL)
	}

	fmt.Printf("\n[STEP 2.2] Running Flow Optimization Solver...\n")
	fmt.Printf("================================================\n")
	fmt.Printf("  Source: %s\n", source)
	fmt.Printf("  Destinations: %v\n", dests)
	fmt.Printf("  Flows per destination: %d\n", solver.flowsPerDest)
	fmt.Printf("  Total capacity: %.2f\n", solver.capacity)
	fmt.Printf("  U_hot (90th percentile): %.2f\n", solver.Uhot)

	paths, err := solver.ComputingMulti(source, dests, "TEST", logger)
	if err != nil {
		t.Fatalf("Error finding paths: %v", err)
	}

	fmt.Printf("\n[STEP 2.3] Path Selection Results:\n")
	fmt.Printf("==================================\n")
	fmt.Printf("\nSelected Paths (Flow Optimization):\n")
	fmt.Printf("%s\n", strings.Repeat("-", 80))
	for i, path := range paths {
		hopStr := strings.Join(path.Hops, " -> ")
		fmt.Printf("  [%d] Rtt=%.4f, RawRTT=%.2fms, Hops=%d: %s\n",
			i+1, path.Rtt, path.RawRTT, len(path.Hops)-1, hopStr)
	}
	fmt.Printf("\n  Total paths selected: %d\n", len(paths))

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

	// Step 4: Verify flow conservation (basic check)
	fmt.Printf("\n[STEP 4] Flow Conservation Check:\n")
	fmt.Printf("==================================\n")
	inFlow := make(map[string]int)
	outFlow := make(map[string]int)

	for _, path := range paths {
		for i := 0; i < len(path.Hops)-1; i++ {
			from := path.Hops[i]
			to := path.Hops[i+1]
			outFlow[from]++
			inFlow[to]++
		}
	}

	// Check conservation for intermediate nodes
	fmt.Printf("  Checking flow conservation at intermediate nodes:\n")
	conservationOK := true
	for node := range nodeLoad {
		if node == source {
			continue // Source has only outgoing flow
		}
		isDest := false
		for _, dest := range dests {
			if node == dest {
				isDest = true
				break
			}
		}
		if isDest {
			continue // Destinations have only incoming flow
		}
		if inFlow[node] != outFlow[node] {
			fmt.Printf("    ❌ Node %s: in=%d, out=%d (VIOLATION)\n", node, inFlow[node], outFlow[node])
			conservationOK = false
		} else {
			fmt.Printf("    ✅ Node %s: in=%d, out=%d (OK)\n", node, inFlow[node], outFlow[node])
		}
	}

	if conservationOK {
		fmt.Printf("\n  ✅ All intermediate nodes satisfy flow conservation\n")
	} else {
		fmt.Printf("\n  ❌ Flow conservation violations detected\n")
	}

	// Step 5: Show edge usage and congestion
	fmt.Printf("\n[STEP 5] Edge Usage and Congestion:\n")
	fmt.Printf("===================================\n")
	edgeUsage := make(map[string]int)
	for _, path := range paths {
		for i := 0; i < len(path.Hops)-1; i++ {
			edgeKey := path.Hops[i] + "->" + path.Hops[i+1]
			edgeUsage[edgeKey]++
		}
	}

	fmt.Printf("  %-35s %s\n", "Edge", "Usage")
	fmt.Printf("  %s\n", strings.Repeat("-", 45))
	for edge, usage := range edgeUsage {
		fmt.Printf("  %-35s %d\n", edge, usage)
	}
	fmt.Printf("\n")
}
