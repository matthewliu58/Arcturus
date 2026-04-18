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
	heapItem   *HeapItem // Reference to the heap item for this batch
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

type sendInfo struct {
	p    *packet.Packet
	next net.IP
}

func (w *worker) handleMsg(msg *aggregatorMsg) {
	var toSend []sendInfo

	w.logger.Info("handleMsg", "routingKey", msg.routingKey, "nextHop", msg.nextHop.String(), "payloadLen", len(msg.data))

	//todo select these two variables by characteristics of business
	buffSize := config.Config_.Aggregator.BufferSize
	batchTimeout := time.Duration(config.Config_.Aggregator.BatchTimeoutMs) * time.Millisecond

	if len(msg.data) >= 1024 {
		msg.emerge = true
		buffSize = len(msg.data) + packet.HeaderSize
	}

	if msg.emerge {
		pkt := packet.NewPacket(buffSize)
		for i, h := range msg.routingInfo.Hops {
			pkt.SetHopIP(i, util.HopIPToNet(h))
		}
		pkt.SetPort(msg.port)
		pkt.SetHopPos(1)
		pkt.AppendUserPacket(msg.userID, msg.data)
		w.flush(pkt, msg.nextHop)
		return
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
			inHeap:     false,
		}

		for i, h := range msg.routingInfo.Hops {
			b.pkt.SetHopIP(i, util.HopIPToNet(h))
		}
		b.pkt.SetPort(msg.port)
		b.pkt.SetHopPos(1)

		w.batches[msg.routingKey] = b
		w.logger.Info("create batch", "routingKey", b.RoutingKey, "nextHop", b.NextHop.String())
	}

	ok := b.pkt.AppendUserPacket(msg.userID, msg.data)
	if !ok {
		if b.inHeap {
			// Remove from heap
			heap.Remove(&w.heap, b.heapItem.index)
			b.heapItem = nil
		}
		toSend = append(toSend, sendInfo{b.pkt, b.NextHop})
		b.pkt = packet.NewPacket(buffSize)
		b.createTime = time.Now()
		b.inHeap = false
		b.heapItem = nil // Reset heap item reference
		b.pkt.AppendUserPacket(msg.userID, msg.data)
	}
	w.logger.Info("add packet success", "routingKey", b.RoutingKey,
		"nextHop", b.NextHop.String(), "payloadLen", b.pkt.PayloadLen)

	if !b.inHeap {
		w.logger.Info("add to heap", "routingKey", b.RoutingKey, "nextHop", b.NextHop.String())
		item := &HeapItem{
			batch:    b,
			deadline: time.Now().Add(batchTimeout),
		}
		heap.Push(&w.heap, item)
		b.inHeap = true
		b.heapItem = item // Store heap item reference
	}

	w.mu.Unlock()

	for _, p := range toSend {
		w.flush(p.p, p.next)
	}
}

func (w *worker) checkTimeout() {

	var toSend []sendInfo

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
		item.batch.heapItem = nil // Reset heap item reference
		toSend = append(toSend, sendInfo{item.batch.pkt, item.batch.NextHop})
		item.batch.pkt = packet.NewPacket(item.batch.BuffSize)
		item.batch.createTime = time.Now()
	}

	w.mu.Unlock()

	for _, p := range toSend {
		w.flush(p.p, p.next)
	}
}

func (w *worker) flush(p *packet.Packet, nextHop net.IP) {

	w.logger.Info("flush batch", slog.Any("port", p.Port), slog.Any("nextHop", nextHop.String()))

	if p == nil || p.PayloadLen == 0 {
		return
	}

	p.SerializeHead()
	buf := p.Buf[:p.TotalBytes()]

	go func() {
		w.logger.Info("send packet", slog.Any("port", p.Port), slog.Any("buf", len(buf)))
		err := manager.TunnelMgr.SendPacket(context.Background(), nextHop, buf, nextHop.String(), w.logger)
		if err != nil {
			w.logger.Error("send packet failed", slog.Any("port", p.Port), slog.Any("err", err))
		}
	}()
}

func (w *worker) evictStaleBatches() {

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
