package http_host

import (
	"context"
	"log/slog"

	"github.com/simon/mneme/internal/capability"
)

// HTTPHostRPC provides Wails-bound methods for ad-hoc static file hosting.
type HTTPHostRPC struct {
	server *Server
}

// NewHTTPHostRPC creates a Wails RPC handler.
func NewHTTPHostRPC(server *Server) *HTTPHostRPC {
	return &HTTPHostRPC{server: server}
}

// RegisterRPC implements capability.WailsRPCRegistrar.
func (r *HTTPHostRPC) RegisterRPC() []interface{} { return []interface{}{r} }

// ServeDir starts serving a directory and returns the access URL.
func (r *HTTPHostRPC) ServeDir(key, dirPath string) (string, error) {
	return r.server.ServeDir(context.Background(), key, dirPath)
}

// Stop stops serving a directory by key.
func (r *HTTPHostRPC) Stop(key string) error {
	return r.server.Stop(key)
}

// List returns all currently served directories.
func (r *HTTPHostRPC) List() map[string]string {
	return r.server.List()
}

// Register registers http_host RPC bindings.
func Register(log *slog.Logger) {
	capability.RegisterWailsRPC(NewHTTPHostRPC(New(log)))
}
