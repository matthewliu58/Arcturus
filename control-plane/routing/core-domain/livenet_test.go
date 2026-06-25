package core_domain

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"control-plane/routing/graph"
)

var lsRttRegex = regexp.MustCompile(`# RTT:\s*(\d+(?:\.\d+)?)ms`)

// lsEdgeRisk calculates edge weight based on CPU pressure, loss, and latency
func lsEdgeRisk(cpuPressure, loss, latency float64) float64 {
	const (
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

func lsParseCost266Edges(filePath string, logger *slog.Logger) []*graph.Edge {
	file, err := os.Open(filePath)
	if err != nil {
		logger.Error("Failed to open file", slog.String("path", filePath), slog.String("error", err.Error()))
		return nil
	}
	defer file.Close()

	var edges []*graph.Edge
	var inLinks bool

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
				if m := lsRttRegex.FindStringSubmatch(line); m != nil {
					fmt.Sscanf(m[1], "%f", &rawRTT)
				}

				// Generate random CPU utilization for each edge
					cpuUtil := float64(GetRandomUtil())
					edgeWeight := lsEdgeRisk(cpuUtil, defaultLoss, rawRTT)

					logger.Debug("Edge created",
						slog.String("source", source),
						slog.String("target", target),
						slog.Float64("rawRTT", rawRTT),
						slog.Float64("cpuUtil", cpuUtil),
						slog.Float64("edgeWeight", edgeWeight))

					edges = append(edges, &graph.Edge{
						SourceIp:      source,
						DestinationIp: target,
						EdgeWeight:    edgeWeight,
						Load:          cpuUtil,
						Latency:       rawRTT,
						Loss:          defaultLoss,
					})
					edges = append(edges, &graph.Edge{
						SourceIp:      target,
						DestinationIp: source,
						EdgeWeight:    edgeWeight,
						Load:          cpuUtil,
						Latency:       rawRTT,
						Loss:          defaultLoss,
					})
				}
			}
		}
	}
	return edges
}

func TestLiveStyleSolver(t *testing.T) {
	// Create a logger that writes to both console and file
	logFile, err := os.Create("livenet_test.log")
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

	topoFile := "evaluation/native"
	edges := lsParseCost266Edges(topoFile, logger)
	if len(edges) == 0 {
		t.Fatal("Failed to parse janos-us-ca topology")
	}

	solver := NewLiveStyleSolver(edges, 2)

	paths, err := solver.Computing("Lisbon", "Warsaw", "TEST", logger)
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	fmt.Printf("Found %d paths\n", len(paths))
}

// TestLiveStyleSolverRandom tests with random source and 10 random destinations
func TestLiveStyleSolverRandom(t *testing.T) {
	rand.Seed(time.Now().UnixNano())

	topoFile := "evaluation/native"

	// Get all unique nodes from topology
	tempLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	edges := lsParseCost266Edges(topoFile, tempLogger)
	if len(edges) == 0 {
		t.Fatal("Failed to parse janos-us-ca topology")
	}

	nodes := make(map[string]bool)
	for _, e := range edges {
		nodes[e.SourceIp] = true
	}
	nodeList := make([]string, 0, len(nodes))
	for node := range nodes {
		nodeList = append(nodeList, node)
	}

	// Select 20 sources with replacement (allow repeats since only 12 nodes)
	numSources := 20
	selectedSources := make([]string, numSources)
	for i := 0; i < numSources; i++ {
		selectedSources[i] = nodeList[rand.Intn(len(nodeList))]
	}

	for sourceIdx, source := range selectedSources {
		// Create log file for this source
		logFileName := fmt.Sprintf("native_livenet_test_random_%d_%d.log", time.Now().Unix(), sourceIdx+1)
		logFile, err := os.Create(logFileName)
		if err != nil {
			t.Fatalf("Failed to create log file: %v", err)
		}

		multiWriter := io.MultiWriter(os.Stdout, logFile)
		logger := slog.New(slog.NewTextHandler(multiWriter, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))

		// Re-parse edges to log them for this run
		edges = lsParseCost266Edges(topoFile, logger)

		// Randomly select 10 unique destinations (excluding source)
		rand.Shuffle(len(nodeList), func(i, j int) { nodeList[i], nodeList[j] = nodeList[j], nodeList[i] })
		var destinations []string
		for _, node := range nodeList {
			if node != source && len(destinations) < 10 {
				destinations = append(destinations, node)
			}
		}

		logger.Info("TestLiveStyleSolverRandom started",
			slog.Int("run", sourceIdx+1),
			slog.String("source", source),
			slog.Int("destinations", len(destinations)))

		solver := NewLiveStyleSolver(edges, 2)

		totalPaths := 0
		totalLatency := 0.0
		for idx, dest := range destinations {
			logger.Info("Processing destination",
				slog.Int("index", idx+1),
				slog.String("destination", dest))

			paths, err := solver.Computing(source, dest, "RANDOM_TEST", logger)
			if err != nil {
				logger.Error("Failed to compute paths",
					slog.String("destination", dest),
					slog.String("error", err.Error()))
				continue
			}

			for _, path := range paths {
				totalLatency += path.RawRTT
			}
			totalPaths += len(paths)
		}

		logger.Info("Test completed",
			slog.Int("run", sourceIdx+1),
			slog.Int("total_paths", totalPaths),
			slog.Float64("avg_latency", totalLatency/float64(totalPaths)))

		fmt.Printf("\n=== Run %d/%d - Random Test Summary ===\n", sourceIdx+1, numSources)
		fmt.Printf("Source: %s\n", source)
		fmt.Printf("Destinations: %v\n", destinations)
		fmt.Printf("Total paths: %d\n", totalPaths)
		fmt.Printf("Average latency: %.2f ms\n", totalLatency/float64(totalPaths))

		logFile.Close()
	}
}
