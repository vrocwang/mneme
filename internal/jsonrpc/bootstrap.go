package jsonrpc

import (
	"log/slog"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/pkg/events"
)

// Bootstrap creates and optionally starts the JSON-RPC server. Returns nil if
// the inference HTTP server is disabled in config.
func Bootstrap(cfg *config.Config, provider inference.Provider, bus *events.Bus, log *slog.Logger) *Server {
	if cfg == nil || !cfg.InferenceHTTP.Enabled {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}

	port := cfg.InferenceHTTP.Port
	if port <= 0 {
		port = 8080
	}
	bind := cfg.InferenceHTTP.Bind
	if bind == "" {
		bind = "127.0.0.1"
	}

	return NewServer(log, bind, port, provider, bus)
}
