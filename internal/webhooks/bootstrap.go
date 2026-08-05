package webhooks

import (
	"log/slog"

	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/pkg/events"
)

// Bootstrap creates and optionally starts the webhook server. Returns nil
// if webhooks are disabled. All business logic lives here — callers only
// wire dependencies and start/stop.
func Bootstrap(cfg *config.Config, tm *TunnelManager, triage *agent.TriagePipeline, bus *events.Bus, log *slog.Logger) *Server {
	if cfg == nil || !cfg.Webhook.Enabled {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}

	port := cfg.Webhook.Port
	if port <= 0 {
		port = 9500
	}

	// Wire the tunnel manager to publish lifecycle events.
	if bus != nil {
		tm.SetEventBus(bus)
	}

	handler := NewDispatchHandler(tm, triage, log)
	server := NewServer(log, port, cfg.Webhook.Secret, handler.Handle)

	return server
}
