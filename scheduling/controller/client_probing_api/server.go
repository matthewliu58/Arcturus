package client_probing_api

import (
	"context"
	"database/sql"
	log "github.com/sirupsen/logrus"
	"net/http"
	"time"
)

// StartServer initializes and starts the Client Delay API HTTP server.
// It supports graceful shutdown via the provided context.
func StartServer(ctx context.Context, db *sql.DB, addr string) {
	handler := NewHandler(db)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Run the server in a separate goroutine so it doesn't block.
	go func() {
		log.Infof("Client Delay API server starting on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Client Delay API server failed to start: %v", err)
		}
	}()

	// Listen for the context cancellation signal to initiate shutdown.
	<-ctx.Done()

	log.Info("Shutting down Client Delay API server...")

	// Create a context with a timeout for the shutdown process.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Errorf("Client Delay API server graceful shutdown failed: %v", err)
	} else {
		log.Info("Client Delay API server stopped gracefully.")
	}
}
