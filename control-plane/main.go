package main

import (
	"bytes"
	"context"
	agg "control-plane/aggregator"
	"control-plane/api"
	rece "control-plane/receive-info"
	"control-plane/routing/graph"
	_client "control-plane/sync/etcd_client"
	_server "control-plane/sync/etcd_server"
	"control-plane/util"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type SourceHandler struct {
	handler slog.Handler
}

func (h *SourceHandler) Handle(ctx context.Context, r slog.Record) error {

	fs := runtime.CallersFrames([]uintptr{r.PC})
	frame, _ := fs.Next()

	fileName := filepath.Base(frame.File)

	r.AddAttrs(
		slog.String("file", fileName),
		slog.Int("line", frame.Line),
		slog.String("func", frame.Func.Name()),
	)

	return h.handler.Handle(ctx, r)
}

func (h *SourceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *SourceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SourceHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h *SourceHandler) WithGroup(name string) slog.Handler {
	return &SourceHandler{handler: h.handler.WithGroup(name)}
}

func HandleRoutingWatchEvent(
	r *graph.GraphManager,
	eventType string,
	key string,
	val string,
	logger *slog.Logger,
) {

	pre := util.GenerateRandomLetters(5)

	if len(val) > 0 {
		compact := new(bytes.Buffer)
		if err := json.Compact(compact, []byte(val)); err != nil {
			logger.Warn("Failed to compress JSON",
				slog.String("pre", pre),
				slog.Any("err", err),
			)
		}
	}

	logger.Info("[WATCH] event",
		slog.String("pre", pre),
		slog.String("eventType", eventType),
		slog.String("key", key),
		slog.String("value", val),
	)

	var tel agg.NodeTelemetry
	if len(val) > 0 {
		if err := json.Unmarshal([]byte(val), &tel); err != nil {
			logger.Warn("Failed to parse node JSON, skipping",
				slog.String("pre", pre),
				slog.String("ip", key),
				slog.Any("error", err),
			)
			return
		}
	}

	switch eventType {
	case "CREATE", "UPDATE":
		r.AddNode(&tel, pre)
		//r.DumpGraph(logPre)

	case "DELETE":
		r.RemoveNode(tel.PublicIP, pre)
		//r.DumpGraph(logPre)

	default:
		logger.Warn("[WATCH] UNKNOWN eventType",
			slog.String("pre", pre),
			slog.String("eventType", eventType),
			slog.String("key", key),
		)
	}
}

func HandleLastWatchEvent(
	globalStats *agg.GlobalStats,
	eventType string,
	key string,
	val string,
	logger *slog.Logger,
) {
	pre := util.GenerateRandomLetters(5)

	logger.Info("[LAST WATCH] event",
		slog.String("pre", pre),
		slog.String("eventType", eventType),
		slog.String("key", key),
	)

	var lastStats rece.LastTelemetry
	if len(val) > 0 {
		if err := json.Unmarshal([]byte(val), &lastStats); err != nil {
			logger.Warn("Failed to parse LastStats JSON, skipping",
				slog.String("pre", pre),
				slog.String("key", key),
				slog.Any("error", err),
			)
			return
		}
	}

	switch eventType {
	case "CREATE", "UPDATE":
		globalStats.AddOrUpdateNode(&lastStats)

	case "DELETE":
		globalStats.DelNode(lastStats.IP)

	default:
		logger.Warn("[LAST WATCH] UNKNOWN eventType",
			slog.String("pre", pre),
			slog.String("eventType", eventType),
			slog.String("key", key),
		)
	}
}

func main() {
	pre := "main"

	logDir := filepath.Join(".", "log")
	if err := os.MkdirAll(logDir, os.ModePerm); err != nil {
		panic("Failed to create log directory: " + err.Error())
	}
	logFilePath := filepath.Join(logDir, "app.log")
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic("Failed to open log file: " + err.Error())
	}
	baseHandler := slog.NewTextHandler(logFile, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})
	logger := slog.New(&SourceHandler{handler: baseHandler})
	slog.SetDefault(logger)

	util.Config_, err = util.ReadYamlConfig(logger)
	if err != nil {
		logger.Error("Failed to read config file",
			slog.String("pre", pre), slog.Any("err", err.Error()))
		return
	}
	uu := util.Config_
	logger.Info("Successfully read config file", slog.String("pre", pre), slog.Any("config", uu))

	if uu.ServerIP != "" {
		nodeName := "etcd-" + strings.ReplaceAll(uu.ServerIP, ".", "-")
		_ = os.RemoveAll(uu.DataDir)
		etcdServer, err := _server.StartEmbeddedEtcd(uu.ServerList, uu.ServerIP,
			uu.DataDir, nodeName, pre, logger)
		if err != nil {
			logger.Error("Failed to start embedded etcd", slog.String("pre", pre), slog.Any("err", err))
		}
		defer etcdServer.Close()
	}

	var serverIps []string
	if len(uu.ServerList) > 0 {
		for _, v := range uu.ServerList {
			serverIps = append(serverIps, v+":2379")
		}
	} else {
		logger.Error("Failed to get serverIps", slog.String("pre", pre),
			slog.Any("serverIps", serverIps))
		return
	}
	cli, err := _client.NewEtcdClient(serverIps, 5*time.Second)
	if err != nil {
		logger.Error("Failed to connect to etcd", slog.String("pre", pre), slog.Any("err", err))
		return
	} else {
		logger.Info("Etcd Client connected", slog.String("pre", pre), slog.Any("serverIps", serverIps))
	}
	defer cli.Close()

	api.SourceTargetMap, err = api.LoadSourceTargetsFromExeDir()
	if err != nil {
		logger.Error("Failed to load cloud storage targets", slog.String("pre", pre),
			slog.Any("err", err.Error()))
		return
	} else {
		logger.Info("Load cloud storage targets success", slog.String("pre", pre),
			slog.Any("targets", api.SourceTargetMap))
	}

	r := graph.NewGraphManager(logger)
	nodeMap, err := _client.GetPrefixAll(cli, "/routing-middle/", pre, logger)
	if err != nil {
		logger.Warn("Failed to get full prefix information", slog.String("pre", pre), slog.Any("err", err))
	} else {
		logger.Info("Successfully got full prefix information", slog.String("pre", pre), slog.Any("nodeMap", nodeMap))
		for k, nodeJson := range nodeMap {
			var tel agg.NodeTelemetry
			if err = json.Unmarshal([]byte(nodeJson), &tel); err != nil {
				logger.Warn("Failed to parse node JSON, skipping", slog.String("pre", pre),
					slog.String("ip", k), slog.Any("err", err))
				continue
			}
			r.AddNode(&tel, pre)
			//r.DumpGraph(logPre)
		}
	}

	_client.WatchPrefix(cli, "/routing-middle/",
		func(eventType, key, val string, logger *slog.Logger) {
			HandleRoutingWatchEvent(r, eventType, key, val, logger)
		}, logger)

	globalStats := agg.NewGlobalStats()
	lastMap, err := _client.GetPrefixAll(cli, "/routing-last/", pre, logger)
	if err != nil {
		logger.Warn("Failed to get full last statistics", slog.String("pre", pre), slog.Any("err", err))
	} else {
		logger.Info("Successfully got full last statistics", slog.String("pre", pre), slog.Any("lastMap", lastMap))
		for _, lastJson := range lastMap {
			var lastStats rece.LastTelemetry
			if err = json.Unmarshal([]byte(lastJson), &lastStats); err != nil {
				continue
			}
			globalStats.AddOrUpdateNode(&lastStats)
		}
	}

	globalStats.StartAggregateWorker(logger)
	_client.WatchPrefix(cli, "/routing-last/",
		func(eventType, key, val string, logger *slog.Logger) {
			HandleLastWatchEvent(globalStats, eventType, key, val, logger)
		}, logger)

	exe, _ := os.Executable()
	storageDir := filepath.Join(filepath.Dir(exe), "vm_storage")
	logger.Info(
		"using storage directory",
		slog.String("pre", pre),
		slog.String("storageDir", storageDir),
	)
	s, _ := util.NewFileStorage(storageDir, 0, pre, logger)
	go agg.CalcClusterWeightedAvg(s, 10*time.Second, cli, pre, logger)

	router := gin.Default()
	router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, "success") })

	api.InitNodeProbeRouter(router, cli, logger)
	api.InitVmReceiveAPIRouter(router, s, logger)
	api.InitLastReceiveAPIRouter(router, cli, logger)
	api.InitUserRoutingRouter(router, r, globalStats, logger)

	logger.Info("service started successfully", slog.String("pre", pre), slog.String("port", ":7081"))
	if err = router.Run(":7081"); err != nil {
		logger.Error("Failed to start service", slog.String("pre", pre), slog.Any("err", err))
		return
	}
}
