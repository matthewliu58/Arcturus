package domain_api

import (
	"database/sql"
	log "github.com/sirupsen/logrus"
	"net/http"
	"time"
)

// Server represents the API server
type Server struct {
	db          *sql.DB
	fileManager FileManager
	addr        string
	server      *http.Server
}

// NewServer creates a new API server
func NewServer(db *sql.DB, fileManager FileManager, addr string) *Server {
	return &Server{
		db:          db,
		fileManager: fileManager,
		addr:        addr,
	}
}

// Start starts the API server
func (s *Server) Start() error {
	// Create routing
	mux := http.NewServeMux()

	// Create handlers
	domainDAO := NewDomainDAO(s.db)
	domainHandler := NewDomainHandler(domainDAO, s.fileManager)

	// Register routes
	mux.HandleFunc("/api/domains", domainHandler.HandleListDomains)
	mux.HandleFunc("/api/domains/add", domainHandler.HandleAddDomain)
	mux.HandleFunc("/api/domains/delete", domainHandler.HandleDeleteDomain)

	// Configure server
	s.server = &http.Server{
		Addr:         s.addr,
		Handler:      LoggingMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start the server
	log.Infof("API server started at %s", s.addr)
	return s.server.ListenAndServe()
}

// Stop stops the API server
func (s *Server) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

// LoggingMiddleware logs HTTP requests
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Infof(
			"LoggingMiddleware, %s %s %s",
			r.Method,
			r.RequestURI,
			time.Since(start),
		)
	})
}
