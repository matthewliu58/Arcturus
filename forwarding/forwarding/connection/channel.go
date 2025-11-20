package connection

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type channelPool struct {
	mu    sync.RWMutex
	conns chan net.Conn

	factory Factory

	config PoolConfig
}

type Factory func() (net.Conn, error)

func NewChannelPool(config PoolConfig, factory Factory) (Pool, error) {
	if config.InitialCap < 0 || config.MaxCap <= 0 || config.InitialCap > config.MaxCap {
		return nil, errors.New("invalid capacity settings")
	}

	c := &channelPool{
		conns:   make(chan net.Conn, config.MaxCap),
		factory: factory,
		config:  config,
	}

	for i := 0; i < config.InitialCap; i++ {
		conn, err := factory()
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("factory is not able to fill the goroutine_pool: %s", err)
		}
		c.conns <- conn
	}
	return c, nil
}

func (c *channelPool) getConnsAndFactory() (chan net.Conn, Factory) {
	c.mu.RLock()
	conns := c.conns
	factory := c.factory
	c.mu.RUnlock()
	return conns, factory
}

func (c *channelPool) Get() (net.Conn, error) {
	return c.GetWithTimeout(c.config.AcquireTimeout)
}

func (c *channelPool) GetWithTimeout(timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return c.GetWithContext(ctx)
}

func (c *channelPool) GetWithContext(ctx context.Context) (net.Conn, error) {
	conns, factory := c.getConnsAndFactory()
	if conns == nil {
		return nil, ErrClosed
	}

	select {
	case conn := <-conns:
		if conn == nil {
			return nil, ErrClosed
		}
		return c.wrapConn(conn), nil

	case <-ctx.Done():

		return nil, ErrTimeout

	default:

		conn, err := factory()
		if err != nil {

			select {
			case conn := <-conns:
				if conn == nil {
					return nil, ErrClosed
				}
				return c.wrapConn(conn), nil
			case <-ctx.Done():
				return nil, ErrTimeout
			}
		}
		return c.wrapConn(conn), nil
	}
}

func (c *channelPool) put(conn net.Conn) error {
	if conn == nil {
		return errors.New("connection is nil. rejecting")
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conns == nil {
		return conn.Close()
	}

	select {
	case c.conns <- conn:
		return nil
	default:
		return conn.Close()
	}
}

func (c *channelPool) Close() {
	c.mu.Lock()
	conns := c.conns
	c.conns = nil
	c.factory = nil
	c.mu.Unlock()

	if conns == nil {
		return
	}

	close(conns)
	for conn := range conns {
		conn.Close()
	}
}

func (c *channelPool) Len() int {
	conns, _ := c.getConnsAndFactory()
	return len(conns)
}
