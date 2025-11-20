package connection

import (
	"context"
	"errors"
	"net"
	"time"
)

var (
	ErrClosed = errors.New("goroutine_pool is closed")

	ErrTimeout = errors.New("connection acquisition timed out")
)

type PoolConfig struct {
	InitialCap int

	MaxCap int

	AcquireTimeout time.Duration
}

type Pool interface {
	Get() (net.Conn, error)

	GetWithTimeout(timeout time.Duration) (net.Conn, error)

	GetWithContext(ctx context.Context) (net.Conn, error)

	Close()

	Len() int
}
