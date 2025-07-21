package connector

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
)

func adminLog() *slog.Logger {
	return slog.With("component", "connector.AdminManager")
}

// AdminManager manages admin HTTP endpoints on a dedicated port
type AdminManager struct {
	mu      sync.Mutex
	started bool
	port    int
	mux     *http.ServeMux
}

// NewAdminManager creates a new AdminManager for the given port
func NewAdminManager(port int) *AdminManager {
	return &AdminManager{
		port: port,
		mux:  http.NewServeMux(),
	}
}

// RegisterHandler registers an HTTP handler at the given path
func (am *AdminManager) RegisterHandler(path string, handler http.Handler) {
	am.mu.Lock()
	defer am.mu.Unlock()

	adminLog().Debug("registering admin handler", "port", am.port, "path", path)
	am.mux.Handle(path, handler)
}

// StartHTTP starts the admin HTTP server
func (am *AdminManager) StartHTTP(ctx context.Context) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.started {
		return
	}
	am.started = true

	if am.port == 0 {
		adminLog().Debug("admin port not configured, skipping admin server")
		return
	}

	// Add a health check endpoint
	am.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	go func() {
		adminLog().Info("starting admin HTTP server", "port", am.port)
		server := &http.Server{
			Addr:    fmt.Sprintf(":%d", am.port),
			Handler: am.mux,
		}

		go func() {
			<-ctx.Done()
			adminLog().Debug("shutting down admin HTTP server")
			if err := server.Close(); err != nil {
				adminLog().Warn("error closing admin HTTP server", "err", err.Error())
			}
		}()

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			adminLog().Error("admin HTTP server ended unexpectedly", "error", err)
		}
	}()
}
