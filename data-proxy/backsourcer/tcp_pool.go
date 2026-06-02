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
				err := pc.conn.Close()
				if err != nil {
					p.logger.Error("Close expired conn failed", slog.String("addr", addr), slog.Any("err", err))
				}
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
			p.logger.Info("Closing expired conn from pool", slog.String("addr", addr))
			err := pc.conn.Close()
			if err != nil {
				p.logger.Error("Close expired conn failed in Get", slog.String("addr", addr), slog.Any("err", err))
			}
			next := e.Next()
			lst.Remove(e)
			e = next
			continue
		}

		lst.Remove(e)
		pc.elem = nil
		p.Unlock()
		_ = pc.conn.SetDeadline(time.Now().Add(ioTimeout))
		p.logger.Info("Reusing conn from pool", slog.String("addr", addr), slog.Int("poolSize", lst.Len()))
		return pc.conn, nil
	}

	p.Unlock()

	p.logger.Info("Creating new conn", slog.String("addr", addr))
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		p.logger.Error("DialTimeout failed", slog.String("addr", addr), slog.Any("err", err))
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
		err := conn.Close()
		if err != nil {
			p.logger.Error("Close conn failed (no pool)", slog.String("addr", addr), slog.Any("err", err))
		}
		return
	}

	if !healthy || lst.Len() >= p.maxIdle {
		err := conn.Close()
		if err != nil {
			p.logger.Error("Close conn failed (unhealthy/too many)", slog.String("addr", addr), slog.Bool("healthy", healthy), slog.Int("poolSize", lst.Len()), slog.Any("err", err))
		}
		return
	}

	pc := &pooledConn{
		conn:     conn,
		created:  time.Now(),
		lastUsed: time.Now(),
	}
	pc.elem = lst.PushFront(pc)
	p.logger.Info("Put conn to pool", slog.String("addr", addr), slog.Int("poolSize", lst.Len()))
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
		t.logger.Error("Get conn from pool failed", slog.String("addr", addr), slog.Any("err", err))
		return nil, err
	}

	_ = conn.SetDeadline(time.Now().Add(t.ioTimeout))

	_, err = conn.Write(reqData)
	if err != nil {
		t.logger.Error("Write to conn failed", slog.String("addr", addr), slog.Any("err", err))
		t.pool.Put(addr, conn, false)
		return nil, err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.logger.Error("ReadString from conn failed", slog.String("addr", addr), slog.Any("err", err))
		t.pool.Put(addr, conn, false)
		return nil, err
	}

	t.pool.Put(addr, conn, true)

	return []byte(line), nil
}
