package etcd_client

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// NewEtcdClient 创建一个 etcd client
func NewEtcdClient(endpoints []string, dialTimeout time.Duration) (*clientv3.Client, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: dialTimeout,
	})
	if err != nil {
		return nil, err
	}
	return cli, nil
}

// PutKey 写入 key
func PutKey(cli *clientv3.Client, key, value, pre string, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := cli.Put(ctx, key, value)
	if err != nil {
		logger.Error("Put error", slog.Any("err", err))
	} else {
		logger.Info("Put", slog.String("pre", pre), key, value)
	}
}

// PutKeyWithLease 简化版：写入 key + TTL lease，不打印日志，也不保活
func PutKeyWithLease(cli *clientv3.Client, key, value string, ttlSeconds int64, pre string, logger *slog.Logger) error {
	// 1. 创建 lease
	leaseResp, err := cli.Grant(context.Background(), ttlSeconds)
	if err != nil {
		logger.Error("Put error", slog.String("pre", pre), slog.Any("err", err))
		return err
	}

	// 2. Put key-value 并附加 lease
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = cli.Put(ctx, key, value, clientv3.WithLease(leaseResp.ID))
	if err != nil {
		logger.Error("Put error", slog.String("pre", pre), slog.Any("err", err))
		return err
	}

	return nil
}

func DeleteKey(cli *clientv3.Client, key string, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := cli.Delete(ctx, key)
	if err != nil {
		logger.Error("Delete error:", err)
		return
	}

	if resp.Deleted > 0 {
		logger.Info("Deleted key %s successfully\n", key)
	} else {
		logger.Warn("Key %s not found\n", key)
	}
}

// GetKey 获取 key
func GetKey(cli *clientv3.Client, key string, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := cli.Get(ctx, key)
	if err != nil {
		logger.Error("Get error:", err)
		return
	}

	for _, kv := range resp.Kvs {
		logger.Info("Get %s = %s\n", string(kv.Key), string(kv.Value))
	}
}

func WatchPrefix(cli *clientv3.Client, prefix string, callback func(eventType, key, value string, logger *slog.Logger), logger *slog.Logger) {
	logger.Info("Start watch prefix", slog.String("prefix", prefix))
	go func() {
		rch := cli.Watch(context.Background(), prefix, clientv3.WithPrefix(), clientv3.WithPrevKV())
		for resp := range rch {
			for _, ev := range resp.Events {
				eventType := ""
				switch ev.Type {
				case clientv3.EventTypePut:
					if ev.IsCreate() {
						eventType = "CREATE"
					} else if ev.IsModify() {
						eventType = "UPDATE"
					}
				case clientv3.EventTypeDelete:
					eventType = "DELETE"
				}
				callback(eventType, string(ev.Kv.Key), string(ev.Kv.Value), logger)
			}
		}
	}()
}

func GetPrefixAll(cli *clientv3.Client, prefix, pre string, logger *slog.Logger) (map[string]string, error) {
	// 1. 构建超时上下文，避免阻塞
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel() // 函数退出释放上下文资源

	// 2. 核心：调用 Get 方法，配合 clientv3.WithPrefix() 实现前缀查询
	// clientv3.WithPrefix()：匹配所有以 prefix 开头的 Key
	resp, err := cli.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		logger.Error("Get prefix all error", slog.String("pre", pre),
			slog.Any("err", err), slog.String("prefix", prefix))
		return nil, err
	}

	if len(resp.Kvs) == 0 {
		logger.Info("No keys found for prefix", slog.String("pre", pre),
			slog.String("prefix", prefix))
		return nil, nil
	}

	// 3. 初始化返回结果（map 存储所有 Key-Value，方便调用方使用）
	prefixData := make(map[string]string, len(resp.Kvs))

	// 4. 遍历查询结果，填充到 map 中
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		value := string(kv.Value)
		prefixData[key] = value

		// 可选：打印日志（与你现有 GetKey 函数的日志风格保持一致）
		//logger.Info("Get prefix data", slog.String("key", key), slog.String("value", value))

		compact := new(bytes.Buffer)
		err := json.Compact(compact, []byte(value))
		if err != nil {
			logger.Warn("压缩 JSON 失败", slog.String("pre", pre), slog.Any("err", err))
			compact.WriteString(value) // 失败就直接原值
		}

		logger.Info("Get prefix data",
			slog.String("pre", pre),
			slog.String("key", key),
			slog.String("value", compact.String()),
		)
	}

	// 5. 日志提示前缀下数据总量
	if len(prefixData) == 0 {
		logger.Warn("No data found under prefix", slog.String("pre", pre), slog.String("prefix", prefix))
	} else {
		logger.Info("Get prefix all success", slog.String("pre", pre),
			slog.String("prefix", prefix), slog.Int("data_count", len(prefixData)))
	}

	return prefixData, nil
}
