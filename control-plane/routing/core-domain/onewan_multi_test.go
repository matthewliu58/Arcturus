package core_domain

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

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
	logFile, err := os.Create("onewan_multi_test.log")
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}
	defer logFile.Close()

	multiWriter := io.MultiWriter(os.Stdout, logFile)
	logger := slog.New(slog.NewTextHandler(multiWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	cost266File := "evaluation/cost266"
	edges := omParseCost266Edges(cost266File, logger)
	if len(edges) == 0 {
		t.Fatal("Failed to parse cost266 topology")
	}

	solver := NewONEWANSolver(edges, 10)

	source := "Amsterdam"
	dests := []string{"Paris", "Berlin"}

	logger.Info("ONEWAN Multi Solver Test",
		slog.String("source", source),
		slog.Any("destinations", dests))

	paths, err := solver.ComputingMulti(source, dests, "TEST", logger)
	if err != nil {
		t.Fatalf("Error finding paths: %v", err)
	}

	logger.Info("Test completed", slog.Int("total_paths", len(paths)))
}
