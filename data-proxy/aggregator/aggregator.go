package aggregator

import (
	"container/heap"
	"context"
	manager "data-proxy/tunnel-manager"
	packet "data-proxy/tunnel-packet"
	"data-proxy/util"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// 配置常量
const (
	BatchTimeout  = 10 * time.Millisecond
	inputChanSize = 100000
	workerCount   = 8
)

// ------------------------------
// 消息结构
// ------------------------------
type aggregatorMsg struct {
	routingKey  string
	port        uint16
	routingInfo util.PathInfo
	nextHop     net.IP
	userID      uint32
	data        []byte
}

// ------------------------------
// Batch 结构体
// ------------------------------
type Batch struct {
	RoutingKey string
	NextHop    net.IP
	pkt        *packet.Packet
	closed     bool
	inHeap     bool
}

// ------------------------------
// 最小堆
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
// Worker 结构体
// ------------------------------
type worker struct {
	batches map[string]*Batch
	heap    MinHeap
	mu      sync.Mutex
	logger  *slog.Logger
}

// ------------------------------
// 全局 Aggregator
// ------------------------------
type Aggregator struct {
	inputChan chan *aggregatorMsg
	workers   []*worker
	wg        sync.WaitGroup
}

// ------------------------------
// 全局单例
// ------------------------------
var GlobalAggRequest *Aggregator
var GlobalAggResponse *Aggregator

// ------------------------------
// NewAggregator
// ------------------------------
func NewAggregator(pre string, l *slog.Logger) *Aggregator {

	l.Info("NewAggregator", "pre", pre)

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
// Start
// ------------------------------
func (a *Aggregator) Start() {
	for _, w := range a.workers {
		//w := w

		// 消息处理协程
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			for msg := range a.inputChan {
				w.handleMsg(msg)
			}
		}()

		// 超时检查协程
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			ticker := time.NewTicker(1 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				w.checkTimeout()
			}
		}()
	}
}

// ------------------------------
// AddToBatch
// ------------------------------
func (a *Aggregator) AddToBatch(
	routingKey string,
	port uint16,
	routingInfo util.PathInfo,
	nextHop net.IP,
	userID uint32,
	data []byte,
) {
	a.inputChan <- &aggregatorMsg{
		routingKey:  routingKey,
		port:        port,
		routingInfo: routingInfo,
		nextHop:     nextHop,
		userID:      userID,
		data:        data,
	}
}

// ------------------------------
// handleMsg
// ------------------------------
func (w *worker) handleMsg(msg *aggregatorMsg) {
	var toSend []*Batch

	w.mu.Lock()

	b := w.batches[msg.routingKey]
	if b == nil {
		b = &Batch{
			RoutingKey: msg.routingKey,
			NextHop:    msg.nextHop,
			pkt:        packet.NewPacket(),
		}

		for i, h := range msg.routingInfo.Hops {
			if i == 3 && !strings.Contains(h, ":") {
				continue
			}
			b.pkt.SetHopIP(i, util.HopIPToNet(h))
		}
		b.pkt.SetPort(msg.port)
		b.pkt.SetHopPos(1)

		w.batches[msg.routingKey] = b
	}

	ok := b.pkt.AppendUserPacket(msg.userID, msg.data)
	if !ok {
		// 包满了：先加入待发送列表
		toSend = append(toSend, b)
		// 重置包
		b.pkt = packet.NewPacket()
		b.inHeap = false
		// 把当前这条重新加进去
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

	w.mu.Unlock()

	// 锁外统一发送
	for _, b := range toSend {
		w.flush(b)
	}
}

// ------------------------------
// checkTimeout
// ------------------------------
func (w *worker) checkTimeout() {
	var toSend []*Batch

	w.mu.Lock()

	now := time.Now()
	for w.heap.Len() > 0 {
		item := w.heap[0]
		if item.deadline.After(now) {
			break
		}

		heap.Pop(&w.heap)
		item.batch.inHeap = false
		toSend = append(toSend, item.batch)
	}

	w.mu.Unlock()

	// 锁外发送
	for _, b := range toSend {
		w.flush(b)
	}
}

// ------------------------------
// flush
// ------------------------------
func (w *worker) flush(b *Batch) {
	if b.closed || b.pkt == nil || b.pkt.PayloadLen == 0 {
		return
	}

	b.pkt.SerializeHead()
	buf := b.pkt.Buf[:b.pkt.TotalBytes()]

	// 异步发送，不阻塞任何逻辑
	go func() {
		_ = manager.TunnelMgr.SendPacket(context.Background(), b.NextHop, buf, "", w.logger)
	}()

	// 重置
	b.pkt = packet.NewPacket()
}
