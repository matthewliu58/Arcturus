package main

import (
	"bytes"
	"context"
	api2 "control-plane/api"
	agg "control-plane/info-agg"
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

// 自定义Handler：修复slog.Context为context.Context，兼容所有Go 1.21+版本
type SourceHandler struct {
	handler slog.Handler
}

// Handle 核心修复：把slog.Context改为context.Context
func (h *SourceHandler) Handle(ctx context.Context, r slog.Record) error {
	// 采集调用日志的位置（跳过当前Handler的栈帧，取真实业务代码的位置）
	fs := runtime.CallersFrames([]uintptr{r.PC})
	frame, _ := fs.Next()

	// 只保留文件名（去掉全路径）
	fileName := filepath.Base(frame.File)

	// 向日志记录中添加源位置字段
	r.AddAttrs(
		slog.String("file", fileName),          // 文件名
		slog.Int("line", frame.Line),           // 行号
		slog.String("func", frame.Func.Name()), // 函数名（可选）
	)

	// 交给底层TextHandler输出
	return h.handler.Handle(ctx, r)
}

// 以下是slog.Handler接口的默认实现（全部修正为context.Context）
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
	// 日志追踪 ID
	logPre := util.GenerateRandomLetters(5)

	// JSON 压缩（仅用于日志可读性）
	if len(val) > 0 {
		compact := new(bytes.Buffer)
		if err := json.Compact(compact, []byte(val)); err != nil {
			logger.Warn("压缩 JSON 失败",
				slog.String("pre", logPre),
				slog.Any("err", err),
			)
		}
	}

	logger.Info("[WATCH] event",
		slog.String("pre", logPre),
		slog.String("eventType", eventType),
		slog.String("key", key),
		slog.String("value", val),
	)

	var tel agg.Telemetry
	if len(val) > 0 {
		if err := json.Unmarshal([]byte(val), &tel); err != nil {
			logger.Warn("解析节点JSON失败，跳过",
				slog.String("pre", logPre),
				slog.String("ip", key),
				slog.Any("error", err),
			)
			return
		}
	}

	switch eventType {
	case "CREATE", "UPDATE":
		r.AddNode(&tel, logPre)
		//r.DumpGraph(logPre)

	case "DELETE":
		r.RemoveNode(tel.PublicIP, logPre)
		//r.DumpGraph(logPre)

	default:
		logger.Warn("[WATCH] UNKNOWN eventType",
			slog.String("pre", logPre),
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

	var lastStats rece.LastStats
	if len(val) > 0 {
		if err := json.Unmarshal([]byte(val), &lastStats); err != nil {
			logger.Warn("解析 LastStats JSON 失败，跳过",
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

	// 创建 log 目录（与 pkg 同级）
	logDir := filepath.Join(".", "log")
	if err := os.MkdirAll(logDir, os.ModePerm); err != nil {
		panic("无法创建日志目录: " + err.Error())
	}

	logFilePath := filepath.Join(logDir, "app.log")

	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic("无法打开日志文件: " + err.Error())
	}

	// 2. 配置基础TextHandler（保留原有Level等配置）
	baseHandler := slog.NewTextHandler(logFile, &slog.HandlerOptions{
		Level:     slog.LevelInfo, // 日志级别
		AddSource: true,           // 必须开启！否则无法获取文件名/行号
	})

	// 3. 包装成自定义SourceHandler（添加文件名、行号、函数名）
	logger := slog.New(&SourceHandler{handler: baseHandler})

	// 4. 设置为全局logger（可选，整个项目都能生效）
	slog.SetDefault(logger)

	// 初始化标记
	logPre := "init"

	// 读取配置文件
	util.Config_, err = util.ReadYamlConfig(logger)
	if err != nil {
		logger.Error("Failed to read config file",
			slog.String("pre", logPre),
			slog.Any("err", err.Error()))
		return
	}
	uu := util.Config_
	b, _ := json.Marshal(uu)
	logger.Info("读取配置文件成功", slog.String("pre", logPre),
		slog.String("config", string(b)))

	// 启动 etcd server端
	if uu.ServerIP != "" {
		// 节点名用 IP 做后缀，保证唯一
		nodeName := "etcd-" + strings.ReplaceAll(uu.ServerIP, ".", "-")
		// 清理残留数据
		_ = os.RemoveAll(uu.DataDir)
		// 启动 etcd server
		etcdServer, err := _server.StartEmbeddedEtcd(uu.ServerList, uu.ServerIP,
			uu.DataDir, nodeName, logPre, logger)
		if err != nil {
			logger.Error("Failed to start embedded etcd",
				slog.String("pre", logPre), slog.Any("err", err))
		}
		defer etcdServer.Close()
	}

	// 初始化etcd客户端
	var serverIps []string
	if len(uu.ServerList) > 0 {
		for _, v := range uu.ServerList {
			serverIps = append(serverIps, v+":2379")
		}
	} else {
		logger.Error("Failed to get serverIps", slog.String("pre", logPre),
			slog.Any("serverIps", serverIps))
		return
	}
	cli, err := _client.NewEtcdClient(serverIps, 5*time.Second)
	if err != nil {
		logger.Error("Failed to connect to etcd", slog.String("pre", logPre), slog.Any("err", err))
		return
	} else {
		logger.Info("Etcd Client connected", slog.String("pre", logPre), slog.Any("serverIps", serverIps))
	}
	defer cli.Close()

	// 1. 获取所有节点探测任务	cloud storage服务器的
	api2.CloudStorageMap, err = api2.LoadCloudStorageTargetsFromExeDir()
	if err != nil {
		logger.Error("Failed to load cloud storage targets", slog.String("pre", logPre),
			slog.Any("err", err.Error()))
		return
	} else {
		logger.Info("Load cloud storage targets success", slog.String("pre", logPre), slog.Any("targets", api2.CloudStorageMap))
	}

	//获取全量前缀信息 然后初始化 routing map
	r := graph.NewGraphManager(logger)
	nodeMap, err := _client.GetPrefixAll(cli, "/routing/middle/", logPre, logger)
	if err != nil {
		logger.Warn("获取全量前缀信息失败", slog.String("pre", logPre), slog.Any("err", err))
	} else {
		logger.Info("获取全量前缀信息成功", slog.String("pre", logPre), slog.Any("nodeMap", nodeMap))
		for k, nodeJson := range nodeMap {
			var tel agg.Telemetry
			if err := json.Unmarshal([]byte(nodeJson), &tel); err != nil {
				logger.Warn("解析节点JSON失败，跳过", slog.String("pre", logPre),
					slog.String("ip", k), slog.Any("err", err))
				continue
			}
			r.AddNode(&tel, logPre)
			//r.DumpGraph(logPre)
		}
	}

	// 监听 /routing/ 前缀 更新routing map
	_client.WatchPrefix(cli, "/routing/middle/",
		func(eventType, key, val string, logger *slog.Logger) {
			HandleRoutingWatchEvent(r, eventType, key, val, logger)
		}, logger)

	// 初始化 GlobalStats 并监听 /routing/last/ 前缀
	globalStats := agg.NewGlobalStats()
	lastMap, err := _client.GetPrefixAll(cli, "/routing/last/", logPre, logger)
	if err != nil {
		logger.Warn("获取全量 last 统计信息失败", slog.String("pre", logPre), slog.Any("err", err))
	} else {
		logger.Info("获取全量 last 统计信息成功", slog.String("pre", logPre), slog.Any("lastMap", lastMap))
		for _, lastJson := range lastMap {
			var lastStats rece.LastStats
			if err := json.Unmarshal([]byte(lastJson), &lastStats); err != nil {
				continue
			}
			globalStats.AddOrUpdateNode(&lastStats)
		}
	}
	// 启动聚合 worker
	globalStats.StartAggregateWorker(logger)
	// 监听 /routing/last/ 前缀
	_client.WatchPrefix(cli, "/routing/last/",
		func(eventType, key, val string, logger *slog.Logger) {
			HandleLastWatchEvent(globalStats, eventType, key, val, logger)
		}, logger)

	//启动virtual queue逻辑
	exe, _ := os.Executable()
	storageDir := filepath.Join(filepath.Dir(exe), "vm_storage")
	logger.Info(
		"using storage directory",
		slog.String("pre", logPre),
		slog.String("storageDir", storageDir),
	)
	s, _ := util.NewFileStorage(storageDir, 0, logPre, logger)
	go agg.CalcClusterWeightedAvg(s, 10*time.Second, cli, logPre, logger)

	// 初始化Gin路由
	router := gin.Default()
	router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, "success") })

	//收集上报的代理节点信息
	api2.InitVmReceiveAPIRouter(router, s, logger)
	//下发探测任务 & 探测结果从 vm上报接口走
	api2.InitNodeProbeRouter(router, cli, logger)
	//client获取 bulk transfer path信息的接口
	api2.InitUserRoutingRouter(router, r, globalStats, logger)
	//接收节点时延统计信息
	api2.InitLastReceiveAPIRouter(router, cli, logger)

	logger.Info("API服务启动成功", slog.String("pre", logPre), slog.String("port", ":7081"))
	if err := router.Run(":7081"); err != nil {
		logger.Error("API服务启动失败", slog.String("pre", logPre), slog.Any("err", err))
		return
	}
}
