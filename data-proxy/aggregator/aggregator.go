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

const (
	inputChanSize = 100000
	workerCount   = 8
	batchMaxAge   = 60 * time.Second
)

type aggregatorMsg struct {
	emerge      bool
	routingKey  string
	port        uint16
	routingInfo util.PathInfo
	nextHop     net.IP
	userID      uint32
	data        []byte
}

type Batch struct {
	BuffSize   int
	RoutingKey string
	NextHop    net.IP
	pkt        *packet.Packet
	closed     bool
	inHeap     bool
	createTime time.Time
}

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

type worker struct {
	batches map[string]*Batch
	heap    MinHeap
	mu      sync.RWMutex
	logger  *slog.Logger
	stopCh  chan struct{}
}

type Aggregator struct {
	inputChan chan *aggregatorMsg
	workers   []*worker
	wg        sync.WaitGroup
}

var GlobalAggRequest *Aggregator
var GlobalAggResponse *Aggregator

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

func (a *Aggregator) Start(pre string, l *slog.Logger) {

	l.Info("Aggregator Start", "pre", pre)

	for _, w := range a.workers {

		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			for msg := range a.inputChan {
				w.handleMsg(msg)
			}
		}()

		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			ticker := time.NewTicker(1 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				w.checkTimeout()
			}
		}()

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

func (w *worker) handleMsg(msg *aggregatorMsg) {
	var toSend []*Batch

	w.logger.Info("handleMsg", "routingKey", msg.routingKey, "nextHop", msg.nextHop.String(), "payloadLen", len(msg.data))

	//todo select these two variables by characteristics of business
	buffSize := config.Config_.Aggregator.BufferSize
	batchTimeout := time.Duration(config.Config_.Aggregator.BatchTimeoutMs) * time.Millisecond

	if len(msg.data) >= 1024 {
		msg.emerge = true
		buffSize = len(msg.data) + packet.HeaderSize
	}

	w.mu.Lock()

	b := w.batches[msg.routingKey]
	if b == nil {
		b = &Batch{
			BuffSize:   buffSize,
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

		w.batches[msg.routingKey] = b //todo one key can map to many batches
		w.logger.Info("create batch", "routingKey", b.RoutingKey, "nextHop", b.NextHop.String())
	}

	if msg.emerge {
		w.flush(b, buffSize)
		return
	}

	ok := b.pkt.AppendUserPacket(msg.userID, msg.data)
	if !ok {
		toSend = append(toSend, b)

		b.pkt = packet.NewPacket(buffSize)

		b.createTime = time.Now()
		b.inHeap = false
		b.pkt.AppendUserPacket(msg.userID, msg.data)
	}
	w.logger.Info("add packet success", "routingKey", b.RoutingKey,
		"nextHop", b.NextHop.String(), "payloadLen", b.pkt.PayloadLen)

	if !b.inHeap {
		w.logger.Info("add to heap", "routingKey", b.RoutingKey, "nextHop", b.NextHop.String())
		heap.Push(&w.heap, &HeapItem{
			batch:    b,
			deadline: time.Now().Add(batchTimeout),
		})
		b.inHeap = true
	}

	w.mu.Unlock()

	for _, b = range toSend {
		w.flush(b, buffSize)
	}
}

func (w *worker) checkTimeout() {

	//w.logger.Info("checkTimeout")

	var toSend []*Batch

	w.mu.Lock()

	now := time.Now()
	for w.heap.Len() > 0 {
		item := w.heap[0]
		if item.deadline.After(now) {
			break
		}
		w.logger.Info("checkTimeout", "routingKey", item.batch.RoutingKey, "nextHop", item.batch.NextHop.String())
		heap.Pop(&w.heap)
		item.batch.inHeap = false
		toSend = append(toSend, item.batch)
	}

	w.mu.Unlock()

	for _, b := range toSend {
		w.flush(b, b.BuffSize)
	}
}

func (w *worker) flush(b *Batch, buffSize int) {

	w.logger.Info("flush batch", "routingKey", b.RoutingKey, "nextHop", b.NextHop.String(), "payloadLen", b.pkt.PayloadLen)

	if b.closed || b.pkt == nil || b.pkt.PayloadLen == 0 {
		return
	}

	b.pkt.SerializeHead()
	buf := b.pkt.Buf[:b.pkt.TotalBytes()]

	go func() {
		w.logger.Info("send packet", "routingKey", b.RoutingKey, "nextHop", b.NextHop.String(), "payloadLen", b.pkt.PayloadLen)
		_ = manager.TunnelMgr.SendPacket(context.Background(), b.NextHop, buf, b.NextHop.String(), w.logger)
	}()

	b.pkt = packet.NewPacket(buffSize)
	b.createTime = time.Now()
}

func (w *worker) evictStaleBatches() {

	//w.logger.Info("evictStaleBatches")

	now := time.Now()

	w.mu.RLock()
	keys := make([]string, 0, len(w.batches))
	for key := range w.batches {
		keys = append(keys, key)
	}
	w.mu.RUnlock()

	for _, key := range keys {

		w.mu.RLock()
		b, exists := w.batches[key]
		w.mu.RUnlock()

		if !exists {
			continue
		}

		if now.Sub(b.createTime) <= batchMaxAge {
			continue
		}

		w.mu.Lock()
		delete(w.batches, key)
		w.mu.Unlock()

		w.logger.Info("evict stale batch", "routingKey", key)
	}
}
