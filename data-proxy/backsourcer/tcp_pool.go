package backsourcer

import (
	"bufio"
	"container/list"
	"log/slog"
	"net"
	"sync"
	"time"
)

type pooledConn struct {
	conn     net.Conn
	created  time.Time
	lastUsed time.Time
	elem     *list.Element
}

type TCPConnPool struct {
	sync.Mutex
	pools       map[string]*list.List
	maxIdle     int
	maxLifetime time.Duration
	cleanPeriod time.Duration
	logger      *slog.Logger
}

type TCPProtocolWithPool struct {
	dialTimeout time.Duration
	ioTimeout   time.Duration
	pool        *TCPConnPool
	logger      *slog.Logger
}

func NewTCPConnPool(maxIdle int, maxLifetime, cleanPeriod time.Duration, l *slog.Logger) *TCPConnPool {
	pool := &TCPConnPool{
		pools:       make(map[string]*list.List),
		maxIdle:     maxIdle,
		maxLifetime: maxLifetime,
		cleanPeriod: cleanPeriod,
		logger:      l,
	}

	go pool.cleanupLoop()

	return pool
}

func (p *TCPConnPool) cleanupLoop() {
	ticker := time.NewTicker(p.cleanPeriod)
	defer ticker.Stop()

	for range ticker.C {
		p.cleanup()
	}
}

func (p *TCPConnPool) cleanup() {
	p.Lock()
	defer p.Unlock()

	now := time.Now()
	for addr, lst := range p.pools {
		for e := lst.Front(); e != nil; {
			pc := e.Value.(*pooledConn)
			if now.Sub(pc.lastUsed) > p.maxLifetime || now.Sub(pc.created) > p.maxLifetime*2 {
				pc.conn.Close()
				next := e.Next()
				lst.Remove(e)
				e = next
			} else {
				e = e.Next()
			}
		}

		if lst.Len() == 0 {
			delete(p.pools, addr)
		}
	}
}

func (p *TCPConnPool) Get(addr string, dialTimeout, ioTimeout time.Duration) (net.Conn, error) {
	p.Lock()

	lst, exists := p.pools[addr]
	if !exists {
		lst = list.New()
		p.pools[addr] = lst
	}

	for e := lst.Front(); e != nil; {
		pc := e.Value.(*pooledConn)
		if now := time.Now(); now.Sub(pc.lastUsed) > p.maxLifetime {
			pc.conn.Close()
			next := e.Next()
			lst.Remove(e)
			e = next
			continue
		}

		lst.Remove(e)
		pc.elem = nil
		p.Unlock()
		_ = pc.conn.SetDeadline(time.Now().Add(ioTimeout))
		return pc.conn, nil
	}

	p.Unlock()

	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, err
	}

	_ = conn.SetDeadline(time.Now().Add(ioTimeout))

	return conn, nil
}

func (p *TCPConnPool) Put(addr string, conn net.Conn, healthy bool) {
	if conn == nil {
		return
	}

	p.Lock()
	defer p.Unlock()

	lst, exists := p.pools[addr]
	if !exists {
		conn.Close()
		return
	}

	if !healthy || lst.Len() >= p.maxIdle {
		conn.Close()
		return
	}

	pc := &pooledConn{
		conn:     conn,
		created:  time.Now(),
		lastUsed: time.Now(),
	}
	pc.elem = lst.PushFront(pc)
}

func (p *TCPConnPool) Close() {
	p.Lock()
	defer p.Unlock()

	for _, lst := range p.pools {
		for e := lst.Front(); e != nil; e = e.Next() {
			pc := e.Value.(*pooledConn)
			pc.conn.Close()
		}
	}
	p.pools = make(map[string]*list.List)
}

func NewTCPProtocolWithPool(pool *TCPConnPool, dialTimeout, ioTimeout time.Duration, l *slog.Logger) *TCPProtocolWithPool {
	return &TCPProtocolWithPool{
		dialTimeout: dialTimeout,
		ioTimeout:   ioTimeout,
		pool:        pool,
		logger:      l,
	}
}

func (t *TCPProtocolWithPool) DoRequest(addr string, reqData []byte) ([]byte, error) {
	conn, err := t.pool.Get(addr, t.dialTimeout, t.ioTimeout)
	if err != nil {
		return nil, err
	}

	_ = conn.SetDeadline(time.Now().Add(t.ioTimeout))

	_, err = conn.Write(reqData)
	if err != nil {
		t.pool.Put(addr, conn, false)
		return nil, err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.pool.Put(addr, conn, false)
		return nil, err
	}

	t.pool.Put(addr, conn, true)

	return []byte(line), nil
}
