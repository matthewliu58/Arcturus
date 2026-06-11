package core_domain

import (
	"bufio"
	"fmt"
	"io"
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
					if idx := strings.Index(line, "# RTT:"); idx != -1 {
						rttStr := strings.TrimSpace(line[idx+6:])
						if len(rttStr) > 2 && rttStr[len(rttStr)-2:] == "ms" {
							fmt.Sscanf(rttStr[:len(rttStr)-2], "%f", &rawRTT)
						}
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
	logFile, err := os.Create("live_style_test.log")
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

	cost266File := "evaluation/cost266"
	edges := lsParseCost266Edges(cost266File, logger)
	if len(edges) == 0 {
		t.Fatal("Failed to parse cost266 topology")
	}

	solver := NewLiveStyleSolver(edges, 2)

	paths, err := solver.Computing("Lisbon", "Warsaw", "TEST", logger)
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	fmt.Printf("Found %d paths\n", len(paths))
}
