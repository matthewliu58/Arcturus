package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type TaskProcessor func(task Task) (string, error)

type TaskWorker struct {
	client     *clientv3.Client
	workerID   string
	processors map[string]TaskProcessor
	config     EtcdConfig
	wg         sync.WaitGroup
}

func NewTaskWorker(config EtcdConfig) (*TaskWorker, error) {

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   config.Endpoints,
		DialTimeout: config.DialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd probing_client: %w", err)
	}

	workerID := fmt.Sprintf("worker-%d", time.Now().Unix())

	return &TaskWorker{
		client:     client,
		workerID:   workerID,
		processors: make(map[string]TaskProcessor),
		config:     config,
	}, nil
}

func (w *TaskWorker) Close() {
	if w.client != nil {
		w.client.Close()
	}
	w.wg.Wait()
}

func (w *TaskWorker) RegisterProcessor(taskType string, processor TaskProcessor) {
	w.processors[taskType] = processor
}

func (w *TaskWorker) RegisterDefaultProcessors() {
	//
	w.RegisterProcessor("math.add", func(task Task) (string, error) {
		var payload struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil {
			return "", fmt.Errorf("invalid payload: %w", err)
		}

		time.Sleep(2 * time.Second)

		result := payload.A + payload.B
		return fmt.Sprintf("%d", result), nil
	})

	w.RegisterProcessor("string.concat", func(task Task) (string, error) {
		var payload struct {
			Str1 string `json:"str1"`
			Str2 string `json:"str2"`
		}
		if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil {
			return "", fmt.Errorf("invalid payload: %w", err)
		}

		time.Sleep(1 * time.Second)

		return payload.Str1 + payload.Str2, nil
	})
}

func (w *TaskWorker) Start(ctx context.Context) error {
	log.Infof("[%s] Worker starting...\n", w.workerID)

	log.Infof("[%s] Registered processors for task types:", w.workerID)
	for taskType := range w.processors {
		log.Infof("[%s] - %s", w.workerID, taskType)
	}

	watchChan := w.client.Watch(ctx, TaskPrefix, clientv3.WithPrefix())

	for {
		select {
		case <-ctx.Done():
			log.Infof("[%s] Worker shutting down...\n", w.workerID)
			return nil

		case resp, ok := <-watchChan:
			if !ok {
				return fmt.Errorf("watch channel closed")
			}

			for _, event := range resp.Events {

				if event.Type == clientv3.EventTypePut {

					w.wg.Add(1)

					go func(ev *clientv3.Event) {
						defer w.wg.Done()
						w.handleTaskEvent(ctx, ev)
					}(event)
				}
			}
		}
	}
}

func (w *TaskWorker) handleTaskEvent(ctx context.Context, event *clientv3.Event) {
	var task Task
	if err := json.Unmarshal(event.Kv.Value, &task); err != nil {
		log.Errorf("[%s] Failed to unmarshal task: %v\n", w.workerID, err)
		return
	}

	if task.Status != "pending" {
		return
	}

	processor, ok := w.processors[task.Type]
	if !ok {
		log.Errorf("[%s] No processor registered for task type: %s\n", w.workerID, task.Type)
		return
	}

	task.Status = "processing"
	taskJSON, _ := json.Marshal(task)
	_, err := w.client.Put(ctx, string(event.Kv.Key), string(taskJSON))
	if err != nil {
		log.Errorf("[%s] Failed to update task status: %v\n", w.workerID, err)
		return
	}

	log.Infof("[%s] Processing task: %s (Type: %s)\n", w.workerID, task.ID, task.Type)

	result, err := processor(task)

	taskResult := TaskResult{
		TaskID:      task.ID,
		CompletedAt: time.Now(),
	}

	if err != nil {
		task.Status = "failed"
		taskResult.Error = err.Error()
		log.Errorf("[%s] Task processing failed: %s - %v\n", w.workerID, task.ID, err)
	} else {
		task.Status = "completed"
		taskResult.Result = result
		log.Infof("[%s] Task completed successfully: %s - Result: %s\n", w.workerID, task.ID, result)
	}

	resultJSON, _ := json.Marshal(taskResult)
	resultKey := ResultPrefix + task.ID
	_, err = w.client.Put(ctx, resultKey, string(resultJSON))
	if err != nil {
		log.Errorf("[%s] Failed to store task result: %v\n", w.workerID, err)
		return
	}

	taskJSON, _ = json.Marshal(task)
	_, err = w.client.Put(ctx, string(event.Kv.Key), string(taskJSON))
	if err != nil {
		log.Errorf("[%s] Failed to update task status after completion: %v\n", w.workerID, err)
		return
	}
}

func (w *TaskWorker) Run(ctx context.Context) error {

	w.RegisterDefaultProcessors()

	return w.Start(ctx)
}

// func main() {

// 	ctx, cancel := context.WithCancel(context.Background())
// 	defer cancel()

// 	sig := make(chan os.Signal, 1)
// 	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
// 	go func() {
// 		<-sig
// 		log.Println("Received termination signal. Shutting down...")
// 		cancel()
// 	}()

// 	worker, err := NewTaskWorker(DefaultEtcdConfig())
// 	if err != nil {
// 		log.Fatalf("Failed to create task worker: %v\n", err)
// 	}
// 	defer worker.Close()

// 	log.Printf("Starting worker with ID: %s\n", worker.workerID)
// 	log.Println("Press Ctrl+C to exit.")

// 	if err := worker.Run(ctx); err != nil {
// 		log.Fatalf("Error running worker: %v\n", err)
// 	}
// }
