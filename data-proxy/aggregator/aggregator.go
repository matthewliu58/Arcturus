package aggregator

import (
	"container/heap"
	"context"
	manager "data-proxy/tunnel-manager"
	packet "data-proxy/tunnel-packet"
	"data-proxy/util"
	"log/slog"
	"net"
	"sync"
	"time"
)

// 配置常量
const (
	BatchTimeout = 10 * time.Millisecond // 10ms 超时强制发送
)

// ------------------------------
// Batch：同一个路由的攒包对象
// ------------------------------
type Batch struct {
	RoutingKey string // 唯一标识：下一跳IP字符串
	NextHop    net.IP // 下一跳真实IP
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
// 全局聚合器（你要的大 map + 堆）
// ------------------------------
type Aggregator struct {
	mu      sync.Mutex
	batches map[string]*Batch // 大 map：key=路由，value=攒包对象
	heap    MinHeap           // 最小堆：管理超时

	wg sync.WaitGroup
}

// ------------------------------
// NewAggregator：初始化
// ------------------------------
func NewAggregator() *Aggregator {
	return &Aggregator{
		batches: make(map[string]*Batch),
		heap:    make(MinHeap, 0),
	}
}

// ------------------------------
// 全局单例
// ------------------------------
var GlobalAgg = NewAggregator()

// ------------------------------
// StartScheduler：启动超时调度器（单协程）
// ------------------------------
func (a *Aggregator) StartScheduler(l *slog.Logger) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(1 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			a.mu.Lock()
			now := time.Now()

			pre := util.GenerateRandomLetters(5)

			// 超时的全部发掉
			for a.heap.Len() > 0 {
				item := a.heap[0]
				if item.deadline.After(now) {
					break
				}

				// 弹出并发送
				heap.Pop(&a.heap)
				a.flushLocked(item.batch, pre, l)
			}

			a.mu.Unlock()
		}
	}()
}

// ------------------------------
// AddToBatch：用户消息扔进来聚合
// ------------------------------
func (a *Aggregator) AddToBatch(
	routingKey string,
	nextHop net.IP,
	userID uint32,
	data []byte, pre string, l *slog.Logger,
) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 1. 从大 map 里拿当前路由的攒包对象
	b := a.batches[routingKey]
	if b == nil {
		// 没有就新建
		b = &Batch{
			RoutingKey: routingKey,
			NextHop:    nextHop,
			pkt:        packet.NewPacket(),
		}
		a.batches[routingKey] = b
	}

	// 2. 尝试写入子包
	ok := b.pkt.AppendUserPacket(userID, data)
	if !ok {
		// 写满了 → 先发掉
		a.flushLocked(b, pre, l)
		// 新建空包继续写
		b.pkt = packet.NewPacket()
		b.pkt.AppendUserPacket(userID, data)
	}

	// 3. 如果堆里还没有这个 batch，就加入堆（10ms 超时）
	// 确保一个 batch 只在堆里出现一次
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
// flushLocked：发送当前大包（必须加锁调用）
// ------------------------------
func (a *Aggregator) flushLocked(b *Batch, pre string, l *slog.Logger) {
	if b.closed || b.pkt == nil || b.pkt.PayloadLen == 0 {
		return
	}

	// 序列化包头
	b.pkt.SerializeHead()
	sendBuf := b.pkt.Buf[:b.pkt.TotalBytes()]

	// 异步通过 QUIC tunnel 发出去
	go func(ip net.IP, buf []byte, pre string, l *slog.Logger) {
		_ = manager.TunnelMgr.SendPacket(context.Background(), ip, buf, pre, l)
	}(b.NextHop, sendBuf, pre, l)

	// 重置空包
	b.pkt = packet.NewPacket()
}
