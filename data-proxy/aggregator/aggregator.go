package aggregator

import (
	"container/heap"
	"context"
	"data-proxy/config"
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
	inputChanSize = 100000
	workerCount   = 8
	batchMaxAge   = 60 * time.Second // batch 存活最大时间
)

// ------------------------------
// 消息结构
// ------------------------------
type aggregatorMsg struct {
	emerge      bool
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
	BuffSize   int
	RoutingKey string
	NextHop    net.IP
	pkt        *packet.Packet
	closed     bool
	inHeap     bool
	createTime time.Time
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
	mu      sync.RWMutex
	logger  *slog.Logger
	stopCh  chan struct{}
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
			stopCh:  make(chan struct{}),
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

		// 老化检查协程：每 10 秒清理超时的 batch
		a.wg.Add(1)
		go func(w *worker) {
			defer a.wg.Done()
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					w.evictStaleBatches()
				case <-w.stopCh:
					return
				}
			}
		}(w)
	}
}

// ------------------------------
// AddToBatch
// ------------------------------
func (a *Aggregator) AddToBatch(
	emerge bool,
	routingKey string,
	port uint16,
	routingInfo util.PathInfo,
	nextHop net.IP,
	userID uint32,
	data []byte,
) {
	a.inputChan <- &aggregatorMsg{
		emerge:      emerge,
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

	buffSize := config.Config_.Aggregator.BufferSize
	batchTimeout := time.Duration(config.Config_.Aggregator.BatchTimeoutMs) * time.Millisecond

	if len(msg.data) >= 1024 {
		msg.emerge = true
		buffSize = len(msg.data) + 2*packet.HeaderSize
	}

	w.mu.Lock()

	b := w.batches[msg.routingKey]
	if b == nil {
		b = &Batch{
			RoutingKey: msg.routingKey,
			NextHop:    msg.nextHop,
			pkt:        packet.NewPacket(buffSize),
			createTime: time.Now(),
		}

		for i, h := range msg.routingInfo.Hops {
			b.pkt.SetHopIP(i, util.HopIPToNet(h))
		}
		b.pkt.SetPort(msg.port)
		b.pkt.SetHopPos(1)

		w.batches[msg.routingKey] = b
	}

	if msg.emerge { // 紧急包，直接发送
		w.flush(b, b.BuffSize)
		return
	}

	ok := b.pkt.AppendUserPacket(msg.userID, msg.data)
	if !ok {
		// 包满了：先加入待发送列表
		toSend = append(toSend, b)
		// 重置包
		b.pkt = packet.NewPacket(buffSize)
		b.createTime = time.Now()
		b.inHeap = false
		// 把当前这条重新加进去
		b.pkt.AppendUserPacket(msg.userID, msg.data)
	}

	// 不在堆里则加入
	if !b.inHeap {
		heap.Push(&w.heap, &HeapItem{
			batch:    b,
			deadline: time.Now().Add(batchTimeout),
		})
		b.inHeap = true
	}

	w.mu.Unlock()

	// 锁外统一发送
	for _, b = range toSend {
		w.flush(b, buffSize)
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
		w.flush(b, b.BuffSize)
	}
}

// ------------------------------
// flush
// ------------------------------
func (w *worker) flush(b *Batch, buffSize int) {
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
	b.pkt = packet.NewPacket(buffSize)
	b.createTime = time.Now()
}

// ------------------------------
// evictStaleBatches 清理超时的 batch
// ------------------------------
func (w *worker) evictStaleBatches() {
	now := time.Now()

	// 1. 极短锁：只复制所有 key
	w.mu.RLock()
	keys := make([]string, 0, len(w.batches))
	for key := range w.batches {
		keys = append(keys, key)
	}
	w.mu.RUnlock()

	// 2. 无锁遍历，逐个判断 + 逐个删除
	for _, key := range keys {
		// 瞬间加锁，只检查这一个
		w.mu.RLock()
		b, exists := w.batches[key]
		w.mu.RUnlock()

		if !exists {
			continue
		}

		// 判断过期
		if now.Sub(b.createTime) <= batchMaxAge {
			continue
		}

		w.mu.Lock()
		delete(w.batches, key)
		w.mu.Unlock()

		// 日志放外面！不占锁！
		w.logger.Info("evict stale batch", "routingKey", key)
	}
}
