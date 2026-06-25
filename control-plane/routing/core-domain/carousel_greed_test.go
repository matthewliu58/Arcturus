package core_domain

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"control-plane/routing/graph"
)

var cgRttRegex = regexp.MustCompile(`# RTT:\s*(\d+(?:\.\d+)?)ms`)

func cgParseCost266Edges(filePath string, logger *slog.Logger) []*graph.Edge {
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
	defer func() {
		if err := scanner.Err(); err != nil {
			logger.Error("Scanner error", slog.String("error", err.Error()))
		}
	}()

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
				if m := cgRttRegex.FindStringSubmatch(line); m != nil {
					fmt.Sscanf(m[1], "%f", &rawRTT)
				}

				cpuUtil := float64(GetRandomUtil())

					logger.Debug("Edge created",
						slog.String("source", source),
						slog.String("target", target),
						slog.Float64("rawRTT", rawRTT),
						slog.Float64("cpuUtil", cpuUtil))

					edges = append(edges, &graph.Edge{
						SourceIp:      source,
						DestinationIp: target,
						EdgeWeight:    rawRTT,
						Load:          cpuUtil,
						Latency:       rawRTT,
						Loss:          defaultLoss,
					})
					edges = append(edges, &graph.Edge{
						SourceIp:      target,
						DestinationIp: source,
						EdgeWeight:    rawRTT,
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

func TestFlowOptimizationSolverMulti(t *testing.T) {
	topoFile := "evaluation/native"

	// Parse topology to get all nodes
	tempLogFile, _ := os.Create("temp_carousel_parse.log")
	defer os.Remove("temp_carousel_parse.log")
	tempLogger := slog.New(slog.NewTextHandler(tempLogFile, &slog.HandlerOptions{Level: slog.LevelDebug}))
	edges := cgParseCost266Edges(topoFile, tempLogger)
	tempLogFile.Close()

	if len(edges) == 0 {
		t.Fatal("Failed to parse janos-us-ca topology")
	}

	// Get unique nodes
	nodeSet := make(map[string]bool)
	for _, edge := range edges {
		nodeSet[edge.SourceIp] = true
	}
	var nodeList []string
	for node := range nodeSet {
		nodeList = append(nodeList, node)
	}

	// Select 20 sources with replacement (allow repeats since only 12 nodes)
	rand.Seed(time.Now().UnixNano())
	numSources := 20
	selectedSources := make([]string, numSources)
	for i := 0; i < numSources; i++ {
		selectedSources[i] = nodeList[rand.Intn(len(nodeList))]
	}

	// Run 20 tests
	for sourceIdx, source := range selectedSources {
		logFileName := fmt.Sprintf("native_carousel_greed_test_%d_%d.log", time.Now().Unix(), sourceIdx+1)
		logFile, err := os.Create(logFileName)
		if err != nil {
			t.Fatalf("Failed to create log file: %v", err)
		}

		multiWriter := io.MultiWriter(os.Stdout, logFile)
		logger := slog.New(slog.NewTextHandler(multiWriter, &slog.HandlerOptions{Level: slog.LevelDebug}))

		// Parse edges with this logger
		edges = cgParseCost266Edges(topoFile, logger)
		if len(edges) == 0 {
			logFile.Close()
			t.Fatal("Failed to parse janos-us-ca topology")
		}

		solver := NewFlowOptimizationSolver(edges)

		// Select 10 random destinations (different from source)
		var dests []string
		rand.Shuffle(len(nodeList), func(i, j int) {
			nodeList[i], nodeList[j] = nodeList[j], nodeList[i]
		})
		for _, node := range nodeList {
			if node != source && len(dests) < 10 {
				dests = append(dests, node)
			}
		}

		logger.Info("Flow Optimization Solver Test",
			slog.String("source", source),
			slog.Int("run", sourceIdx+1),
			slog.Int("total_runs", numSources),
			slog.Any("destinations", dests))

		paths, err := solver.ComputingMulti(source, dests, "TEST", logger)
		if err != nil {
			logger.Error("Error finding paths", slog.String("error", err.Error()))
			logFile.Close()
			continue
		}

		logger.Info("Test completed", slog.Int("total_paths", len(paths)))
		logFile.Close()
	}
}
