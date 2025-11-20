package connection

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

type mockConn struct {
	closed bool
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	return 0, errors.New("mock connection: read not implemented")
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return len(b), nil
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234}
}

func (m *mockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5678}
}

func (m *mockConn) SetDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func mockConnFactory() Factory {
	return func() (net.Conn, error) {
		return &mockConn{}, nil
	}
}

func errorFactory() Factory {
	return func() (net.Conn, error) {
		return nil, errors.New("factory error")
	}
}

func TestNewChannelPool(t *testing.T) {
	tests := []struct {
		name        string
		config      PoolConfig
		factory     Factory
		shouldError bool
	}{
		{
			name: "",
			config: PoolConfig{
				InitialCap:     2,
				MaxCap:         5,
				AcquireTimeout: time.Second,
			},
			factory:     mockConnFactory(),
			shouldError: false,
		},
		{
			name: "",
			config: PoolConfig{
				InitialCap:     10,
				MaxCap:         5,
				AcquireTimeout: time.Second,
			},
			factory:     mockConnFactory(),
			shouldError: true,
		},
		{
			name: "",
			config: PoolConfig{
				InitialCap:     0,
				MaxCap:         0,
				AcquireTimeout: time.Second,
			},
			factory:     mockConnFactory(),
			shouldError: true,
		},
		{
			name: "",
			config: PoolConfig{
				InitialCap:     -1,
				MaxCap:         5,
				AcquireTimeout: time.Second,
			},
			factory:     mockConnFactory(),
			shouldError: true,
		},
		{
			name: "",
			config: PoolConfig{
				InitialCap:     3,
				MaxCap:         5,
				AcquireTimeout: time.Second,
			},
			factory:     errorFactory(),
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := NewChannelPool(tt.config, tt.factory)
			if tt.shouldError {
				if err == nil {
					t.Errorf("，")
				}
			} else {
				if err != nil {
					t.Errorf("，: %v", err)
				}
				if pool == nil {
					t.Errorf("")
				} else {
					if pool.Len() != tt.config.InitialCap {
						t.Errorf(" %d,  %d", tt.config.InitialCap, pool.Len())
					}
				}
			}
		})
	}
}

func TestChannelPool_Get(t *testing.T) {

	config := PoolConfig{
		InitialCap:     2,
		MaxCap:         3,
		AcquireTimeout: 100 * time.Millisecond,
	}
	pool, err := NewChannelPool(config, mockConnFactory())
	if err != nil {
		t.Fatalf(": %v", err)
	}

	for i := 0; i < config.InitialCap; i++ {
		conn, err := pool.Get()
		if err != nil {
			t.Errorf("，: %v", err)
		}
		if conn == nil {
			t.Errorf("")
		}

	}

	conn, err := pool.Get()
	if err != nil {
		t.Errorf("，: %v", err)
	}
	if conn == nil {
		t.Errorf("")
	}

	pool.Close()
}

func TestChannelPool_GetWithTimeout(t *testing.T) {

	factoryWithLimit := func() Factory {
		var created int
		return func() (net.Conn, error) {
			if created >= 1 {
				// ，
				return nil, errors.New("cannot create more connections")
			}
			created++
			return &mockConn{}, nil
		}
	}()

	config := PoolConfig{
		InitialCap:     1,
		MaxCap:         1,
		AcquireTimeout: 500 * time.Millisecond,
	}

	pool, _ := NewChannelPool(config, factoryWithLimit)

	conn1, err := pool.Get()
	if err != nil {
		t.Fatalf(": %v", err)
	}

	_, err = pool.GetWithTimeout(100 * time.Millisecond)
	if err != ErrTimeout {
		t.Errorf("，: %v", err)
	}

	conn1.Close()
	pool.Close()
}

func TestChannelPool_GetWithContext(t *testing.T) {

	factoryWithLimit := func() Factory {
		var created int
		return func() (net.Conn, error) {
			if created >= 1 {
				//
				return nil, errors.New("cannot create more connections")
			}
			created++
			return &mockConn{}, nil
		}
	}()

	config := PoolConfig{
		InitialCap:     1,
		MaxCap:         1,
		AcquireTimeout: 500 * time.Millisecond,
	}

	pool, _ := NewChannelPool(config, factoryWithLimit)

	conn1, _ := pool.Get()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := pool.GetWithContext(ctx)
	if err != ErrTimeout {
		t.Errorf("，: %v", err)
	}

	conn1.Close()
	pool.Close()
}

func TestPoolConn_Close(t *testing.T) {

	factory := mockConnFactory()
	config := PoolConfig{
		InitialCap:     1,
		MaxCap:         2,
		AcquireTimeout: 500 * time.Millisecond,
	}

	pool, _ := NewChannelPool(config, factory)

	conn, _ := pool.Get()

	if pool.Len() != 0 {
		t.Errorf("0， %d ", pool.Len())
	}

	conn.Close()

	if pool.Len() != 1 {
		t.Errorf("1， %d ", pool.Len())
	}

	conn, _ = pool.Get()

	poolConn, ok := conn.(*PoolConn)
	if !ok {
		t.Fatalf("PoolConn")
	}
	poolConn.MarkUnusable()

	conn.Close()

	if pool.Len() != 0 {
		t.Errorf("0， %d ", pool.Len())
	}

	pool.Close()
}

func TestGetOrCreatePool(t *testing.T) {

	originalCreateFactory := CreateConnectionFactory

	CreateConnectionFactory = func(addr string) Factory {
		return func() (net.Conn, error) {
			return &mockConn{}, nil
		}
	}

	defer func() {
		CreateConnectionFactory = originalCreateFactory
	}()

	poolMu.Lock()
	poolMap = make(map[string]*Pool)
	poolMu.Unlock()

	addr := "test-controller:8080"

	pool1, err := GetOrCreatePool(addr)
	if err != nil {
		t.Fatalf(": %v", err)
	}

	pool2, err := GetOrCreatePool(addr)
	if err != nil {
		t.Fatalf(": %v", err)
	}

	if pool1 != pool2 {
		t.Errorf("")
	}

	customConfig := PoolConfig{
		InitialCap:     2,
		MaxCap:         4,
		AcquireTimeout: 1 * time.Second,
	}

	addr2 := "test-controller-2:9090"
	pool3, err := GetOrCreatePool(addr2, customConfig)
	if err != nil {
		t.Fatalf(": %v", err)
	}

	(*pool1).Close()
	(*pool3).Close()
}

func TestPoolClose(t *testing.T) {

	factory := mockConnFactory()
	config := PoolConfig{
		InitialCap:     3,
		MaxCap:         5,
		AcquireTimeout: 500 * time.Millisecond,
	}

	pool, _ := NewChannelPool(config, factory)

	conn, _ := pool.Get()

	pool.Close()

	_, err := pool.Get()
	if err != ErrClosed {
		t.Errorf("ErrClosed，: %v", err)
	}

	conn.Close()

	pool.Close()
}

func TestPoolUnderHighLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("")
	}

	factory := mockConnFactory()
	config := PoolConfig{
		InitialCap:     5,
		MaxCap:         10,
		AcquireTimeout: 500 * time.Millisecond,
	}

	pool, _ := NewChannelPool(config, factory)

	concurrency := 20
	done := make(chan bool)

	for i := 0; i < concurrency; i++ {
		go func() {

			conn, err := pool.Get()
			if err != nil && err != ErrTimeout {
				t.Errorf(": %v", err)
			}

			if err == nil {

				time.Sleep(10 * time.Millisecond)
				conn.Close()
			}

			done <- true
		}()
	}

	for i := 0; i < concurrency; i++ {
		<-done
	}

	if pool.Len() > config.MaxCap {
		t.Errorf(" %d  %d", pool.Len(), config.MaxCap)
	}

	pool.Close()
}
