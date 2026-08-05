// Package http_host provides a lightweight HTTP server for hosting static
// directories, used by the agent to serve generated content or file previews.
package http_host

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// serverEntry holds a running server and its URL.
type serverEntry struct {
	srv *http.Server
	url string
}

// Server wraps an HTTP server for hosting static directories.
type Server struct {
	mu      sync.RWMutex
	servers map[string]*serverEntry // key → entry
	log     *slog.Logger
}

// New creates a new HTTP host manager.
func New(log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		servers: make(map[string]*serverEntry),
		log:     log.With("component", "http-host"),
	}
}

// ServeDir starts serving a directory on a random local port.
// Returns the URL (http://127.0.0.1:<port>) that can be used to access the content.
// The key can be used to stop the server later via Stop.
func (s *Server) ServeDir(ctx context.Context, key, dirPath string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop existing server for this key if any
	if existing, ok := s.servers[key]; ok {
		existing.srv.Close()
	}

	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if info, err := os.Stat(absPath); err != nil || !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", absPath)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	mux := http.NewServeMux()
	fs := http.FileServer(http.Dir(absPath))
	mux.Handle("/", http.StripPrefix("/", fs))

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	s.servers[key] = &serverEntry{srv: server, url: url}
	s.log.Info("serving directory", "key", key, "url", url, "path", absPath)

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.log.Warn("http host server error", "key", key, "error", err)
		}
	}()

	return url, nil
}

// Stop stops the HTTP server for the given key.
func (s *Server) Stop(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.servers[key]
	if !ok {
		return fmt.Errorf("no server for key: %s", key)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := entry.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	delete(s.servers, key)
	s.log.Info("stopped serving", "key", key)
	return nil
}

// StopAll stops all running HTTP servers.
func (s *Server) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for key, entry := range s.servers {
		entry.srv.Shutdown(ctx)
		s.log.Info("stopped serving", "key", key)
	}
	s.servers = make(map[string]*serverEntry)
}

// List returns running server keys and URLs.
func (s *Server) List() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string)
	for key, entry := range s.servers {
		out[key] = entry.url
	}
	return out
}
