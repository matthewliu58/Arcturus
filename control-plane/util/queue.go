package util

import (
	"container/list"
	"log/slog"
	"sync"
)

// ----------------------- FixedQueue -----------------------
// 线程安全固定大小队列，支持自动弹出最老元素
type FixedQueue struct {
	list   *list.List
	size   int
	mu     sync.Mutex
	logger *slog.Logger
}

// NewFixedQueue 创建固定大小队列
func NewFixedQueue(size int, logger *slog.Logger) *FixedQueue {
	if size <= 0 {
		panic("size must be greater than 0")
	}
	return &FixedQueue{
		list:   list.New(),
		size:   size,
		logger: logger,
	}
}

// Push 入队，如果满了会自动弹出最老的元素
func (q *FixedQueue) Push(value interface{}) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.list.Len() >= q.size {
		q.list.Remove(q.list.Front())
	}
	q.list.PushBack(value)
}

// Pop 弹出最老的元素
func (q *FixedQueue) Pop() interface{} {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.list.Len() == 0 {
		return nil
	}
	e := q.list.Front()
	q.list.Remove(e)
	return e.Value
}

func (q *FixedQueue) Latest() interface{} {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.list.Len() == 0 {
		return nil
	}
	return q.list.Back().Value
}

// Len 返回队列当前长度
func (q *FixedQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.list.Len()
}

// Print 打印队列内容
func (q *FixedQueue) Print(pre string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for e := q.list.Front(); e != nil; e = e.Next() {
		q.logger.Info("Queue", slog.String("pre", pre), slog.Any("data", e.Value))
	}
}

// SnapshotLatestFirst 返回队列中所有元素的切片（按最新到最老顺序）
func (q *FixedQueue) SnapshotLatestFirst() []interface{} {
	q.mu.Lock()
	defer q.mu.Unlock()

	arr := make([]interface{}, 0, q.list.Len())
	for e := q.list.Back(); e != nil; e = e.Prev() {
		arr = append(arr, e.Value)
	}
	return arr
}
