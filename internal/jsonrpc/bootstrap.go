package jsonrpc

import (
	"log/slog"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/inference"
	"github.com/simon/mneme/internal/security"
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

	srv := NewServer(log, bind, port, provider, bus)

	// Attach a pairing guard so the HTTP surface is authenticated. If the guard
	// cannot be created the server still starts but fails closed on every
	// non-health request (see requireAuth).
	guard, err := security.NewPairingGuard(cfg.Workspace, log)
	if err != nil {
		log.Warn("failed to create pairing guard; HTTP endpoints will be unavailable", "error", err)
		return srv
	}
	// Accept an operator-supplied token from the environment when present.
	if envToken, ok := security.LoadTokenFromEnv(); ok {
		guard.AddToken(envToken)
	}
	srv.SetAuthGuard(guard)
	return srv
}
