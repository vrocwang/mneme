// Package jsonrpc provides an HTTP JSON-RPC 2.0 server for headless operation,
// SSE event streaming, and an OpenAI-compatible /v1/chat/completions endpoint.
package jsonrpc

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/security"
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
	guard    *security.PairingGuard
	limiter  *security.ActionTracker
}

// SetAuthGuard attaches the pairing guard used to authenticate requests.
// Without a guard the server fails closed for all non-health endpoints.
func (s *Server) SetAuthGuard(guard *security.PairingGuard) {
	s.guard = guard
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
		// 300 requests per minute per client IP is generous for local tooling
		// while bounding abuse from a compromised process or browser tab.
		limiter: security.NewActionTracker(time.Minute, 300),
	}
}

// Start begins listening for HTTP requests.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	// All non-health endpoints require a valid bearer token and are rate
	// limited per client IP. CORS headers are intentionally not emitted so
	// browser-based cross-origin clients (including DNS-rebinding attacks)
	// cannot read responses.
	protected := s.requireAuth
	mux.Handle("/api/rpc", s.rateLimit(protected(http.HandlerFunc(s.handleRPC))))
	mux.Handle("/api/events", s.rateLimit(protected(http.HandlerFunc(s.handleSSE))))
	mux.Handle("/v1/chat/completions", s.rateLimit(protected(http.HandlerFunc(s.handleCompletions))))
	mux.Handle("/v1/models", s.rateLimit(protected(http.HandlerFunc(s.handleModels))))

	s.srv = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", s.bind, s.port),
		Handler:      mux,
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
