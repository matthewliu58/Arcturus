package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	TaskPrefix   = "/metrics_tasks/"
	ResultPrefix = "/results/"
)

type Task struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Payload   string    `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"` // "pending", "processing", "completed", "failed"
}

type TaskResult struct {
	TaskID      string    `json:"task_id"`
	Result      string    `json:"result"`
	Error       string    `json:"error,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
}

type EtcdConfig struct {
	Endpoints   []string
	DialTimeout time.Duration
}

func DefaultEtcdConfig() EtcdConfig {
	return EtcdConfig{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	}
}

type TaskPublisher struct {
	client      *clientv3.Client
	publisherID string
	config      EtcdConfig
}

func NewTaskPublisher(config EtcdConfig) (*TaskPublisher, error) {

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   config.Endpoints,
		DialTimeout: config.DialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd probing_client: %w", err)
	}

	publisherID := fmt.Sprintf("publisher-%d", time.Now().Unix())

	return &TaskPublisher{
		client:      client,
		publisherID: publisherID,
		config:      config,
	}, nil
}

func (p *TaskPublisher) Close() {
	if p.client != nil {
		p.client.Close()
	}
}

func (p *TaskPublisher) CreateTask(taskType, payload string) Task {
	return Task{
		ID:        fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Type:      taskType,
		Payload:   payload,
		CreatedAt: time.Now(),
		Status:    "pending",
	}
}

func (p *TaskPublisher) PublishTask(ctx context.Context, task Task) error {
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	taskKey := TaskPrefix + task.ID
	_, err = p.client.Put(ctx, taskKey, string(taskJSON))
	if err != nil {
		return fmt.Errorf("failed to publish task: %w", err)
	}

	log.Infof("[%s] Task published: %s (Type: %s)\n", p.publisherID, task.ID, task.Type)
	return nil
}

func (p *TaskPublisher) GetTaskResult(ctx context.Context, taskID string) (*TaskResult, error) {
	resultKey := ResultPrefix + taskID
	resp, err := p.client.Get(ctx, resultKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get task result: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("no result found for task: %s", taskID)
	}

	var result TaskResult
	if err := json.Unmarshal(resp.Kvs[0].Value, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task result: %w", err)
	}

	return &result, nil
}

func (p *TaskPublisher) WatchTaskResults(ctx context.Context, taskID string) <-chan *TaskResult {
	resultKey := ResultPrefix + taskID
	resultChan := make(chan *TaskResult)

	go func() {
		defer close(resultChan)

		watchChan := p.client.Watch(ctx, resultKey)
		for resp := range watchChan {
			for _, event := range resp.Events {
				if event.Type == clientv3.EventTypePut {
					var result TaskResult
					if err := json.Unmarshal(event.Kv.Value, &result); err != nil {
						log.Warningf("Failed to unmarshal task result: %v\n", err)
						continue
					}

					select {
					case resultChan <- &result:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return resultChan
}

func (p *TaskPublisher) WaitForResult(ctx context.Context, taskID string, timeout time.Duration) (*TaskResult, error) {
	resultChan := p.WatchTaskResults(ctx, taskID)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case result := <-resultChan:
		return result, nil
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout waiting for result")
		}
		return nil, ctx.Err()
	}
}

func (p *TaskPublisher) Run(ctx context.Context) error {
	log.Infof("[%s] Publisher started\n", p.publisherID)

	mathTask := p.CreateTask("math.add", `{"a": 10, "b": 20}`)
	if err := p.PublishTask(ctx, mathTask); err != nil {
		return fmt.Errorf("failed to publish math task: %w", err)
	}

	log.Infof("[%s] Waiting for result of task: %s\n", p.publisherID, mathTask.ID)
	result, err := p.WaitForResult(ctx, mathTask.ID, 30*time.Second)
	if err != nil {
		log.Errorf("[%s] Error: %v\n", p.publisherID, err)
	} else {
		log.Infof("[%s] Math task completed. Result: %s, Error: %s\n",
			p.publisherID, result.Result, result.Error)
	}

	strTask := p.CreateTask("string.concat", `{"str1": "Hello, ", "str2": "World!"}`)
	if err := p.PublishTask(ctx, strTask); err != nil {
		return fmt.Errorf("failed to publish string task: %w", err)
	}

	log.Infof("[%s] Waiting for result of task: %s\n", p.publisherID, strTask.ID)
	result, err = p.WaitForResult(ctx, strTask.ID, 30*time.Second)
	if err != nil {
		log.Errorf("[%s] Error: %v\n", p.publisherID, err)
	} else {
		log.Infof("[%s] String task completed. Result: %s, Error: %s\n",
			p.publisherID, result.Result, result.Error)
	}

	return nil
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

// 	publisher, err := NewTaskPublisher(DefaultEtcdConfig())
// 	if err != nil {
// 		log.Fatalf("Failed to create task publisher: %v\n", err)
// 	}
// 	defer publisher.Close()

// 	if err := publisher.Run(ctx); err != nil {
// 		log.Fatalf("Error running publisher: %v\n", err)
// 	}

// 	log.Println("Publisher completed successfully")
// }
