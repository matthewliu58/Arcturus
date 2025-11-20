// utils/network.go
package utils

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"scheduling/goroutine_pool"
	"time"
)

func CreateGRPCConnection(addr string, timeout time.Duration) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return grpc.DialContext(
		ctx,
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
}

func ReleasePoolResources() {

	goroutine_pool.ReleaseAllPools()
}

type ShutdownHandler struct {
	handlers []func()
}

func NewShutdownHandler(handlers ...func()) ShutdownHandler {
	return ShutdownHandler{
		handlers: handlers,
	}
}

func (h ShutdownHandler) ExecuteShutdown() {
	for _, handler := range h.handlers {
		handler()
	}
}
