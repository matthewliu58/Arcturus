package traefik_server

import (
	"context"
	"net/http"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

// The original RunServer function remains unchanged for backward compatibility
func RunServer() {
	ctx := context.Background()
	RunServerWithContext(ctx)
}

// RunServerWithContext starts the HTTP server with context support
// Configuration is now generated dynamically per-request based on Traefik node's region
func RunServerWithContext(ctx context.Context) {
	log.Info("Starting Traefik dynamic config server (region-based mode)")

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/traefik-dynamic-config", traefikConfigHandler) // Endpoint polled by Traefik

	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8090" // Default port
	}
	listenAddr := ":" + port

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	// Start HTTP server in a goroutine
	serverErrors := make(chan error, 1)
	go func() {
		log.Infof("Starting Traefik config API server on %s", listenAddr)
		log.Infof("Traefik should poll: http://<this-server-ip>:%s/traefik-dynamic-config", port)
		log.Info("Configuration is generated dynamically per-request based on Traefik node's region")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	// Main loop - handle context cancellation and server errors
	// Note: No periodic config refresh needed - configs are generated on-demand
	for {
		select {
		case <-ctx.Done():
			log.Info("Context canceled. Shutting down Traefik config server...")

			// Create shutdown context with timeout
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Gracefully shutdown the HTTP server
			if err := server.Shutdown(shutdownCtx); err != nil {
				log.Infof("Server forced to shutdown: %v", err)
			} else {
				log.Info("Traefik config server stopped gracefully.")
			}
			return

		case err := <-serverErrors:
			log.Errorf("Server error: %v", err)
			return
		}
	}
}

// If you need a version that can be stopped externally
func RunServerWithShutdown() (*http.Server, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	// Setup server (same as above)
	mux := http.NewServeMux()
	mux.HandleFunc("/traefik-dynamic-config", traefikConfigHandler)

	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8090"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	// Start server with context in goroutine
	go RunServerWithContext(ctx)

	return server, cancel
}
