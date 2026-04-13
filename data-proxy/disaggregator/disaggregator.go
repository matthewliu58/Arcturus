package disaggregator

import (
	"log/slog"
	"sync"
)

type Disaggregator struct {
	mu      sync.RWMutex
	waiters map[uint32]chan []byte // userID => 等待通道
}

var GlobalDisagg *Disaggregator

func NewDisaggregator(pre string, l *slog.Logger) *Disaggregator {
	l.Info("NewDisaggregator", "pre", pre)
	return &Disaggregator{
		waiters: make(map[uint32]chan []byte),
	}
}

// Register 本协程注册自己的 chan
func (d *Disaggregator) Register(userID uint32) (<-chan []byte, func()) {
	ch := make(chan []byte, 1)

	d.mu.Lock()
	d.waiters[userID] = ch
	d.mu.Unlock()

	cleanup := func() {
		d.mu.Lock()
		delete(d.waiters, userID)
		close(ch)
		d.mu.Unlock()
	}

	return ch, cleanup
}

// Deliver 隧道收到回包后调用：把数据丢给对应协程
func (d *Disaggregator) Deliver(userID uint32, data []byte) {
	d.mu.RLock()
	ch, ok := d.waiters[userID]
	d.mu.RUnlock()

	if ok {
		ch <- data
	}
}
