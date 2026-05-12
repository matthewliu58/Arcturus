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
	inputChanSize         = 100000
	aggregatorWorkerCount = 8 //todo temporarily change for testing
	batchMaxAge           = 60 * time.Second
)

type aggregatorMsg struct {
	emerge      bool
	routingKey  string
	protocol    string
	port        uint16
	routingInfo util.PathInfo
	nextHop     net.IP
	userID      uint32
	data        []byte
}

type Batch struct {
	mu         sync.RWMutex
	BuffSize   int
	RoutingKey string
	NextHop    net.IP
	pkt        *packet.Packet
	heapItem   *HeapItem
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
	batches map[string][]*Batch // TODO: collect PayloadLen at timeout to adjust initial BuffSize, and adjust batch count per key
	heap    MinHeap
	mu      sync.RWMutex
	id      int
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

	aggregatorCount := config.Config_.AggregatorCount
	if aggregatorCount <= 0 {
		aggregatorCount = aggregatorWorkerCount
	}

	l.Info("NewAggregator", "pre", pre, "aggregatorCount", aggregatorCount)

	agg := &Aggregator{
		inputChan: make(chan *aggregatorMsg, inputChanSize),
		workers:   make([]*worker, aggregatorCount),
	}

	for i := 0; i < aggregatorCount; i++ {
		agg.workers[i] = &worker{
			batches: make(map[string][]*Batch),
			heap:    make(MinHeap, 0),
			id:      i,
			stopCh:  make(chan struct{}),
		}
	}
	return agg
}

func (a *Aggregator) Start(pre string, logger *slog.Logger) {
	logger.Info("Aggregator Start", "pre", pre)

	for _, w := range a.workers {
		a.wg.Add(1)
		go func(ww *worker) {
			defer a.wg.Done()
			for msg := range a.inputChan {
				ww.handleMsg(msg, logger)
			}
		}(w)

		a.wg.Add(1)
		go func(ww *worker) {
			defer a.wg.Done()
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				ww.checkTimeout(logger)
			}
		}(w)

		a.wg.Add(1)
		go func(ww *worker) {
			defer a.wg.Done()
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					ww.evictStaleBatches(logger)
				case <-ww.stopCh:
					return
				}
			}
		}(w)
	}
}

func (a *Aggregator) AddToBatch(
	emerge bool,
	routingKey string,
	protocol string,
	port uint16,
	routingInfo util.PathInfo,
	nextHop net.IP,
	userID uint32,
	data []byte,
) {
	a.inputChan <- &aggregatorMsg{
		emerge:      emerge,
		routingKey:  routingKey,
		protocol:    protocol,
		port:        port,
		routingInfo: routingInfo,
		nextHop:     nextHop,
		userID:      userID,
		data:        data,
	}
}

type sendInfo struct {
	p          *packet.Packet
	routingKey string
	next       net.IP
}

func (w *worker) handleMsg(msg *aggregatorMsg, logger *slog.Logger) {
	logger.Info("handleMsg", slog.Int("workId", w.id), slog.Any("userID", msg.userID),
		"routingKey", msg.routingKey, "payloadLen", len(msg.data))

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
		pkt.SetProtocol(msg.protocol)
		pkt.SetHopPos(1)
		pkt.AppendUserPacket(msg.userID, msg.data)
		w.flush(pkt, msg.routingKey, msg.nextHop, logger)
		return
	}

	var b *Batch = nil
	w.mu.RLock()
	bList := w.batches[msg.routingKey]
	lList := len(bList)
	if lList > 0 {
		if lList <= 1 {
			b = bList[0]
		} else {
			idx := int(msg.userID) % lList
			b = bList[idx]
		}
	}
	w.mu.RUnlock()

	if b == nil {

		var batches []*Batch
		num, ok_ := config.BatchNumMap[msg.port]
		if !ok_ || num <= 0 {
			num = 1
		}
		for i := 0; i < num; i++ {
			b_ := &Batch{
				BuffSize:   buffSize,
				RoutingKey: msg.routingKey,
				NextHop:    msg.nextHop,
				pkt:        packet.NewPacket(buffSize),
				createTime: time.Now(),
			}
			for j, h := range msg.routingInfo.Hops {
				b_.pkt.SetHopIP(j, util.HopIPToNet(h))
			}
			b_.pkt.SetPort(msg.port)
			b_.pkt.SetProtocol(msg.protocol)
			b_.pkt.SetHopPos(1)
			batches = append(batches, b_)
		}

		w.mu.Lock()
		var ok bool
		bList, ok = w.batches[msg.routingKey]
		if !ok {
			w.batches[msg.routingKey] = batches
			logger.Info("create batch", slog.Int("workId", w.id), slog.Any("userID", msg.userID))
		}
		bList, _ = w.batches[msg.routingKey]
		lList = len(bList)
		if lList > 0 {
			if lList <= 1 {
				b = bList[0]
			} else {
				idx := int(msg.userID % uint32(lList))
				b = bList[idx]
			}
		}
		w.mu.Unlock()
	}

	if b != nil {

		b.mu.Lock()
		if b.heapItem == nil ||
			!b.pkt.AppendUserPacket(msg.userID, msg.data) {

			b_ := &Batch{
				BuffSize:   buffSize,
				RoutingKey: msg.routingKey,
				NextHop:    msg.nextHop,
				pkt:        packet.NewPacket(buffSize),
				createTime: time.Now(),
			}
			for j, h := range msg.routingInfo.Hops {
				b_.pkt.SetHopIP(j, util.HopIPToNet(h))
			}
			b_.pkt.SetPort(msg.port)
			b_.pkt.SetProtocol(msg.protocol)
			b_.pkt.SetHopPos(1)

			w.mu.Lock()
			w.batches[msg.routingKey] = append(w.batches[msg.routingKey], b_)
			w.mu.Unlock()

			b = b_
			b.pkt.AppendUserPacket(msg.userID, msg.data)
		}
		logger.Info("add packet", slog.Int("workId", w.id), slog.Any("userID", msg.userID))

		if b.heapItem == nil {
			item := &HeapItem{batch: b, deadline: time.Now().Add(batchTimeout)}
			w.mu.Lock()
			heap.Push(&w.heap, item)
			w.mu.Unlock()
			b.heapItem = item
		}
		b.mu.Unlock()

	} else {
		logger.Error("create batch failed", slog.Int("workId", w.id), slog.Any("userID", msg.userID))
		return
	}
}

func (w *worker) checkTimeout(logger *slog.Logger) {

	now := time.Now()
	var expired []*HeapItem

	w.mu.Lock()
	for w.heap.Len() > 0 {
		b := w.heap[0]
		if b.deadline.After(now) {
			break
		}
		popItem := heap.Pop(&w.heap).(*HeapItem)
		popItem.batch.heapItem = nil
		expired = append(expired, popItem)
	}
	w.mu.Unlock()

	var toSend []sendInfo
	for _, item := range expired {
		b := item.batch
		b.mu.Lock()
		toSend = append(toSend, sendInfo{b.pkt, b.RoutingKey, b.NextHop})
		b.pkt.Wp = packet.HeaderSize
		b.createTime = now
		b.heapItem = nil
		b.mu.Unlock()
	}

	for _, p := range toSend {
		w.flush(p.p, p.routingKey, p.next, logger)
	}
}

func (w *worker) flush(p *packet.Packet, routingKey string, nextHop net.IP, logger *slog.Logger) {

	logger.Info("flush batch", slog.Int("workId", w.id), slog.Any("port", p.Port))

	if p.PayloadLen == 0 {
		logger.Warn("empty batch", slog.Int("workId", w.id), slog.Any("port", p.Port))
		return
	}
	p.SerializeHead()
	buf := p.Buf[:p.TotalBytes()]

	go func() {
		logger.Info("send packet", slog.Int("workId", w.id), slog.String("routingKey", routingKey),
			slog.Any("port", p.Port), slog.Any("buf", len(buf)))
		err := manager.TunnelMgr.SendPacket(context.Background(), nextHop, buf, nextHop.String(), logger)
		if err != nil {
			logger.Error("send packet failed", slog.Int("workId", w.id), slog.Any("port", p.Port), slog.Any("nextHop", nextHop))
		}
	}()
}

func (w *worker) evictStaleBatches(logger *slog.Logger) {
	now := time.Now()

	batches := make(map[string][]*Batch)
	w.mu.RLock()
	for k, v := range w.batches {
		batches[k] = v
	}
	w.mu.RUnlock()

	for k, v := range batches {

		bDel := false
		for _, b := range v {
			stale := now.Sub(b.createTime) > batchMaxAge
			if !stale {
				continue
			} else {
				bDel = true
			}
		}

		if bDel {
			w.mu.Lock()
			for _, b := range v {
				if b.heapItem != nil {
					heap.Remove(&w.heap, b.heapItem.index)
				}
			}
			delete(w.batches, k)
			w.mu.Unlock()
			logger.Info("evict stale batch", slog.Int("workId", w.id), "routingKey", k)
		}
	}
}
