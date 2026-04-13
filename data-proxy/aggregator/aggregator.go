package aggregator

import (
	"container/heap"
	"context"
	manager "data-proxy/tunnel-manager"
	packet "data-proxy/tunnel-packet"
	"log/slog"
	"net"
	"sync"
	"time"
)

// 配置常量
const (
	BatchTimeout  = 10 * time.Millisecond
	inputChanSize = 100000
	workerCount   = 8 // 多个处理协程，可调整
)

// ------------------------------
// 消息结构（你原来的）
// ------------------------------
type aggregatorMsg struct {
	routingKey string
	nextHop    net.IP
	userID     uint32
	data       []byte
}

// ------------------------------
// Batch 结构体（你原来的 + inHeap 标记）
// ------------------------------
type Batch struct {
	RoutingKey string
	NextHop    net.IP
	pkt        *packet.Packet
	closed     bool
	inHeap     bool // 标记是否在堆中，O(1) 判断
}

// ------------------------------
// 最小堆（你原来的不动）
// ------------------------------
type HeapItem struct {
	batch    *Batch
	deadline time.Time
	index    int
}

type MinHeap []*HeapItem

func (h MinHeap) Len() int           { return len(h) }
func (h MinHeap) Less(i, j int) bool { return h[i].deadline.Before(h[j].deadline) }
func (h MinHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *MinHeap) Push(x any) {
	n := len(*h)
	item := x.(*HeapItem)
	item.index = n
	*h = append(*h, item)
}

func (h *MinHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[0 : n-1]
	return item
}

// ------------------------------
// 每个 Worker 独立一套：map + heap
// ------------------------------
type worker struct {
	batches map[string]*Batch
	heap    MinHeap
	mu      sync.Mutex
	logger  *slog.Logger
}

// ------------------------------
// 全局 Aggregator（你原来的结构风格）
// ------------------------------
type Aggregator struct {
	inputChan chan *aggregatorMsg
	workers   []*worker
	wg        sync.WaitGroup
}

// ------------------------------
// 全局单例
// ------------------------------
var GlobalAgg *Aggregator

// ------------------------------
// NewAggregator 创建多协程聚合器
// ------------------------------
func NewAggregator(l *slog.Logger) *Aggregator {
	agg := &Aggregator{
		inputChan: make(chan *aggregatorMsg, inputChanSize),
		workers:   make([]*worker, workerCount),
	}

	for i := 0; i < workerCount; i++ {
		agg.workers[i] = &worker{
			batches: make(map[string]*Batch),
			heap:    make(MinHeap, 0),
			logger:  l.With("worker", i),
		}
	}

	return agg
}

// ------------------------------
// Start 启动所有 worker + 超时调度
// ------------------------------
func (a *Aggregator) Start() {
	// 启动 workerCount 个协程，共同消费同一个管道
	for i := 0; i < workerCount; i++ {
		w := a.workers[i]
		a.wg.Add(1)

		go func(w *worker) {
			defer a.wg.Done()
			for msg := range a.inputChan {
				w.handleMsg(msg)
			}
		}(w)

		// 每个 worker 自己一个超时检查协程
		a.wg.Add(1)
		go func(w *worker) {
			defer a.wg.Done()
			ticker := time.NewTicker(1 * time.Millisecond)
			defer ticker.Stop()

			for range ticker.C {
				w.checkTimeout()
			}
		}(w)
	}
}

// ------------------------------
// AddToBatch 用户接口：只扔管道，不关心哪个协程处理
// ------------------------------
func (a *Aggregator) AddToBatch(
	routingKey string,
	nextHop net.IP,
	userID uint32,
	data []byte,
) {
	a.inputChan <- &aggregatorMsg{
		routingKey: routingKey,
		nextHop:    nextHop,
		userID:     userID,
		data:       data,
	}
}

// ------------------------------
// 单个 worker 处理消息（你原来的逻辑不动）
// ------------------------------
func (w *worker) handleMsg(msg *aggregatorMsg) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 取或创建 Batch
	b := w.batches[msg.routingKey]
	if b == nil {
		b = &Batch{
			RoutingKey: msg.routingKey,
			NextHop:    msg.nextHop,
			pkt:        packet.NewPacket(),
		}
		w.batches[msg.routingKey] = b
	}

	// 尝试追加子包
	ok := b.pkt.AppendUserPacket(msg.userID, msg.data)
	if !ok {
		w.flush(b)
		b.pkt = packet.NewPacket()
		b.pkt.AppendUserPacket(msg.userID, msg.data)
	}

	// 不在堆里则加入
	if !b.inHeap {
		heap.Push(&w.heap, &HeapItem{
			batch:    b,
			deadline: time.Now().Add(BatchTimeout),
		})
		b.inHeap = true
	}
}

func (w *worker) checkTimeout() {
	w.mu.Lock()

	now := time.Now()
	var toSend []*Batch

	// 1. 只把超时的 batch 摘出来，不做任何发送
	for w.heap.Len() > 0 {
		item := w.heap[0]
		if item.deadline.After(now) {
			break
		}

		heap.Pop(&w.heap)
		item.batch.inHeap = false
		toSend = append(toSend, item.batch)
	}

	w.mu.Unlock() //立刻解锁

	// 2. 锁已经释放，再批量发送，完全不阻塞新消息
	for _, b := range toSend {
		w.flush(b)
	}
}

func (w *worker) flush(b *Batch) {
	if b.pkt == nil || b.pkt.PayloadLen == 0 {
		return
	}

	b.pkt.SerializeHead()
	buf := b.pkt.Buf[:b.pkt.TotalBytes()]

	// 异步 QUIC 发送，完全无锁
	go func(ip net.IP, data []byte) {
		_ = manager.TunnelMgr.SendPacket(context.Background(), ip, data, "", w.logger)
	}(b.NextHop, buf)

	// 重置包
	b.pkt = packet.NewPacket()
}
