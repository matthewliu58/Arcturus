package core_domain

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"control-plane/routing/graph"
)

func omParseCost266Edges(filePath string, logger *slog.Logger) []*graph.Edge {
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
					if idx := strings.Index(line, "# RTT:"); idx != -1 {
						rttStr := strings.TrimSpace(line[idx+6:])
						if len(rttStr) > 2 && rttStr[len(rttStr)-2:] == "ms" {
							fmt.Sscanf(rttStr[:len(rttStr)-2], "%f", &rawRTT)
						}
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

func TestONEWANMultiSolver(t *testing.T) {
	rand.Seed(time.Now().UnixNano())

	topoFile := "evaluation/janos-us-ca"

	// Get all unique nodes from topology
	tempLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	edges := omParseCost266Edges(topoFile, tempLogger)
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

	// Shuffle nodes to select 20 unique sources
	rand.Shuffle(len(nodeList), func(i, j int) { nodeList[i], nodeList[j] = nodeList[j], nodeList[i] })

	numSources := 20
	if len(nodeList) < numSources {
		numSources = len(nodeList)
	}
	selectedSources := nodeList[:numSources]

	for sourceIdx, source := range selectedSources {
		logFileName := fmt.Sprintf("janos-us-ca_onewan_multi_test_%d_%d.log", time.Now().Unix(), sourceIdx+1)
		logFile, err := os.Create(logFileName)
		if err != nil {
			t.Fatalf("Failed to create log file: %v", err)
		}

		multiWriter := io.MultiWriter(os.Stdout, logFile)
		logger := slog.New(slog.NewTextHandler(multiWriter, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))

		edges = omParseCost266Edges(topoFile, logger)

		// Randomly select 10 unique destinations
		rand.Shuffle(len(nodeList), func(i, j int) { nodeList[i], nodeList[j] = nodeList[j], nodeList[i] })
		var dests []string
		for _, node := range nodeList {
			if node != source && len(dests) < 10 {
				dests = append(dests, node)
			}
		}

		logger.Info("ONEWAN Multi Solver Test",
			slog.Int("run", sourceIdx+1),
			slog.String("source", source),
			slog.Any("destinations", dests))

		solver := NewONEWANSolver(edges, 10)
		paths, err := solver.ComputingMulti(source, dests, "TEST", logger)
		if err != nil {
			logger.Error("Error finding paths", slog.String("error", err.Error()))
			logFile.Close()
			continue
		}

		logger.Info("Test completed",
			slog.Int("run", sourceIdx+1),
			slog.Int("total_paths", len(paths)))

		fmt.Printf("\n=== Run %d/%d - ONEWAN Multi Test Summary ===\n", sourceIdx+1, numSources)
		fmt.Printf("Source: %s\n", source)
		fmt.Printf("Destinations: %v\n", dests)
		fmt.Printf("Total paths: %d\n", len(paths))

		logFile.Close()
	}
}
