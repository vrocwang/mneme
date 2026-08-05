// Package jsonrpc provides an HTTP JSON-RPC 2.0 server for headless operation,
// SSE event streaming, and an OpenAI-compatible /v1/chat/completions endpoint.
package jsonrpc

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/pkg/events"
)

// Server listens for JSON-RPC and chat completion requests on a local HTTP port.
type Server struct {
	log      *slog.Logger
	port     int
	bind     string
	srv      *http.Server
	provider inference.Provider
	bus      *events.Bus
	registry *MethodRegistry
}

// NewServer creates a JSON-RPC server.
func NewServer(log *slog.Logger, bind string, port int, provider inference.Provider, bus *events.Bus) *Server {
	return &Server{
		log:      log,
		bind:     bind,
		port:     port,
		provider: provider,
		bus:      bus,
		registry: newMethodRegistry(),
	}
}

// Start begins listening for HTTP requests.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/rpc", s.handleRPC)
	mux.HandleFunc("/api/events", s.handleSSE)
	mux.HandleFunc("/v1/chat/completions", s.handleCompletions)
	mux.HandleFunc("/v1/models", s.handleModels)

	s.srv = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", s.bind, s.port),
		Handler:      withCORS(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute, // long timeout for streaming responses
		IdleTimeout:  60 * time.Second,
	}

	s.log.Info("JSON-RPC server starting", "addr", s.srv.Addr)
	return s.srv.ListenAndServe()
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	if s.srv != nil {
		s.log.Info("JSON-RPC server stopping")
		return s.srv.Close()
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Return available models in OpenAI format.
	w.Write([]byte(`{"object":"list","data":[]}`))
}

// withCORS wraps a handler with permissive CORS headers for local development.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
