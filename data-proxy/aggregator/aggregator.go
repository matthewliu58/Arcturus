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
	inputChanSize = 100000 // 管道大小，根据并发调整
)

// ------------------------------
// 从用户协程 → 聚合协程 的消息结构
// ------------------------------
type aggregatorMsg struct {
	routingKey string
	nextHop    net.IP
	userID     uint32
	data       []byte
	//pre        string
	//l          *slog.Logger
}

// ------------------------------
// Batch：同一个路由的攒包对象
// ------------------------------
type Batch struct {
	RoutingKey string
	NextHop    net.IP
	pkt        *packet.Packet
	closed     bool
}

// ------------------------------
// 最小堆实现（按超时时间排序）
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
// 全局聚合器
// ------------------------------
type Aggregator struct {
	mu      sync.Mutex
	batches map[string]*Batch
	heap    MinHeap

	// 你要的管道：用户只写，agg 协程只读
	inputChan chan *aggregatorMsg

	wg sync.WaitGroup
}

// ------------------------------
// NewAggregator
// ------------------------------
func NewAggregator() *Aggregator {
	return &Aggregator{
		batches:   make(map[string]*Batch),
		heap:      make(MinHeap, 0),
		inputChan: make(chan *aggregatorMsg, inputChanSize),
	}
}

// ------------------------------
// 全局单例
// ------------------------------
var GlobalAgg = NewAggregator()

// ------------------------------
// StartScheduler：启动两个循环
// 1. 从管道拉消息
// 2. 超时检查
// ------------------------------
func (a *Aggregator) StartScheduler(l *slog.Logger) {
	// 协程1：只从管道消费消息
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for msg := range a.inputChan {
			a.handleMsg(msg, l)
		}
	}()

	// 协程2：超时调度
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(1 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			a.mu.Lock()
			now := time.Now()
			//pre := util.GenerateRandomLetters(5)
			pre := ""

			for a.heap.Len() > 0 {
				item := a.heap[0]
				if item.deadline.After(now) {
					break
				}
				heap.Pop(&a.heap)
				a.flushLocked(item.batch, pre, l)
			}

			a.mu.Unlock()
		}
	}()
}

// ------------------------------
// 用户侧接口：只往管道发，不做任何逻辑
// ------------------------------
func (a *Aggregator) AddToBatch(
	routingKey string,
	nextHop net.IP,
	userID uint32,
	data []byte,
// pre string,
// l *slog.Logger,
) {
	// 只投递到管道
	a.inputChan <- &aggregatorMsg{
		routingKey: routingKey,
		nextHop:    nextHop,
		userID:     userID,
		data:       data,
		//pre:        pre,
		//l:          l,
	}
}

// ------------------------------
// 真正处理消息：只有 agg 协程会调用
// ------------------------------
func (a *Aggregator) handleMsg(msg *aggregatorMsg, l *slog.Logger) {
	a.mu.Lock()
	defer a.mu.Unlock()

	b := a.batches[msg.routingKey]
	if b == nil {
		b = &Batch{
			RoutingKey: msg.routingKey,
			NextHop:    msg.nextHop,
			pkt:        packet.NewPacket(),
		}
		a.batches[msg.routingKey] = b
	}

	ok := b.pkt.AppendUserPacket(msg.userID, msg.data)
	if !ok {
		a.flushLocked(b, "", l)
		b.pkt = packet.NewPacket()
		b.pkt.AppendUserPacket(msg.userID, msg.data)
	}

	// 检查是否已经在堆里
	inHeap := false
	for _, item := range a.heap {
		if item.batch == b {
			inHeap = true
			break
		}
	}
	if !inHeap {
		heap.Push(&a.heap, &HeapItem{
			batch:    b,
			deadline: time.Now().Add(BatchTimeout),
		})
	}
}

// ------------------------------
// flushLocked 不变
// ------------------------------
func (a *Aggregator) flushLocked(b *Batch, pre string, l *slog.Logger) {
	if b.closed || b.pkt == nil || b.pkt.PayloadLen == 0 {
		return
	}

	b.pkt.SerializeHead()
	sendBuf := b.pkt.Buf[:b.pkt.TotalBytes()]

	go func(ip net.IP, buf []byte, pre string, l *slog.Logger) {
		_ = manager.TunnelMgr.SendPacket(context.Background(), ip, buf, pre, l)
	}(b.NextHop, sendBuf, pre, l)

	b.pkt = packet.NewPacket()
}
