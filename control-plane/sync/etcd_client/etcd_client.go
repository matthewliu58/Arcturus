package etcd_client

import (
	"bytes"
	"context"
	"encoding/json"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log/slog"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

//func NewEtcdClient(endpoints []string, dialTimeout time.Duration) (*clientv3.Client, error) {
//	cli, err := clientv3.New(clientv3.Config{
//		Endpoints:   endpoints,
//		DialTimeout: dialTimeout,
//	})
//	if err != nil {
//		return nil, err
//	}
//	return cli, nil
//}

func NewEtcdClient(endpoints []string, dialTimeout time.Duration) (*clientv3.Client, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:            endpoints,
		DialTimeout:          dialTimeout,
		AutoSyncInterval:     10 * time.Second,
		DialKeepAliveTime:    5 * time.Second,
		DialKeepAliveTimeout: 3 * time.Second,
		DialOptions: []grpc.DialOption{
			grpc.WithBlock(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	})
	return cli, err
}

func PutKey(cli *clientv3.Client, key, value, pre string, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := cli.Put(ctx, key, value)
	if err != nil {
		logger.Error("Put error", slog.Any("err", err))
	} else {
		logger.Info("Put", slog.String("pre", pre), slog.String(key, value))
	}
}

func PutKeyWithLease(cli *clientv3.Client, key, value string, ttlSeconds int64, pre string, logger *slog.Logger) error {

	leaseResp, err := cli.Grant(context.Background(), ttlSeconds)
	if err != nil {
		logger.Error("Put error", slog.String("pre", pre), slog.Any("err", err))
		return err
	}

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
		logger.Error("Delete error", slog.Any("err", err))
		return
	}

	if resp.Deleted > 0 {
		logger.Info("Deleted key successfully", slog.String("key", key))
	} else {
		logger.Warn("Key not found", slog.String("key", key))
	}
}

func GetKey(cli *clientv3.Client, key string, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := cli.Get(ctx, key)
	if err != nil {
		logger.Error("Get error", slog.Any("err", err))
		return
	}

	for _, kv := range resp.Kvs {
		logger.Info("Get key = val", slog.String(string(kv.Key), string(kv.Value)))
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

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

	prefixData := make(map[string]string, len(resp.Kvs))

	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		value := string(kv.Value)
		prefixData[key] = value

		//logger.Info("Get prefix data", slog.String("key", key), slog.String("value", value))

		compact := new(bytes.Buffer)
		err = json.Compact(compact, []byte(value))
		if err != nil {
			logger.Warn("Compact failed", slog.String("pre", pre), slog.Any("err", err))
			compact.WriteString(value)
		}

		logger.Info("Get prefix data",
			slog.String("pre", pre),
			slog.String("key", key),
			slog.String("value", compact.String()),
		)
	}

	if len(prefixData) == 0 {
		logger.Warn("No data found under prefix", slog.String("pre", pre), slog.String("prefix", prefix))
	} else {
		logger.Info("Get prefix all success", slog.String("pre", pre),
			slog.String("prefix", prefix), slog.Int("data_count", len(prefixData)))
	}

	return prefixData, nil
}
