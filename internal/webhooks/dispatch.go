package webhooks

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/simon/mneme/internal/agent"
)

// DispatchHandler implements webhooks.Handler for routing events through
// tunnels to agents or echo targets. It is the bridge between the HTTP
// server and the triage pipeline.
type DispatchHandler struct {
	tm     *TunnelManager
	triage *agent.TriagePipeline
	log    *slog.Logger
}

// NewDispatchHandler creates a handler that routes webhook events through
// tunnel registrations into the triage system.
func NewDispatchHandler(tm *TunnelManager, triage *agent.TriagePipeline, log *slog.Logger) *DispatchHandler {
	if log == nil {
		log = slog.Default()
	}
	return &DispatchHandler{tm: tm, triage: triage, log: log}
}

// Handle processes an incoming webhook event. It looks up the tunnel
// registration by TunnelUUID and dispatches based on target kind.
func (h *DispatchHandler) Handle(event Event) error {
	h.log.Info("webhook dispatch", "id", event.ID, "tunnel", event.TunnelUUID, "source", event.Source)

	// No tunnel UUID → echo (debug/unknown sender).
	if event.TunnelUUID == "" {
		h.log.Debug("webhook: no tunnel UUID, treating as echo")
		return h.handleEcho(event)
	}

	reg, err := h.tm.GetTunnel(event.TunnelUUID)
	if err != nil || reg == nil {
		h.log.Warn("webhook: unknown tunnel, falling back to echo", "tunnel", event.TunnelUUID)
		return h.handleEcho(event)
	}

	switch reg.Target {
	case TargetEcho:
		return h.handleEcho(event)
	case TargetAgent:
		return h.handleAgent(event, reg)
	case TargetSkill:
		return h.handleSkill(event, reg)
	default:
		h.log.Warn("webhook: unknown target, falling back to echo", "target", reg.Target)
		return h.handleEcho(event)
	}
}

func (h *DispatchHandler) handleEcho(event Event) error {
	h.log.Debug("webhook echo", "id", event.ID)
	h.tm.RecordActivity(WebhookActivityEntry{
		ID:           event.ID,
		TunnelUUID:   event.TunnelUUID,
		RequestID:    event.ID,
		Status:       200,
		ResponseSize: len(event.Payload),
		CreatedAt:    time.Now().UTC(),
	})
	return nil
}

func (h *DispatchHandler) handleAgent(event Event, reg *TunnelRegistration) error {
	h.log.Info("webhook routing to agent", "agent_id", reg.TargetID, "event_id", event.ID)

	envelope := &agent.TriageEnvelope{
		ID:          event.ID,
		Source:      "webhook",
		EventKind:   event.EventType,
		Payload:     string(event.Payload),
		ContentType: event.Headers["Content-Type"],
		ReceivedAt:  event.ReceivedAt,
	}

	if h.triage != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		decision, err := h.triage.Process(ctx, envelope)
		if err != nil {
			h.tm.RecordActivity(WebhookActivityEntry{
				ID:         event.ID,
				TunnelUUID: event.TunnelUUID,
				RequestID:  event.ID,
				Status:     500,
				Error:      err.Error(),
				CreatedAt:  time.Now().UTC(),
			})
			return err
		}
		h.tm.RecordActivity(WebhookActivityEntry{
			ID:           event.ID,
			TunnelUUID:   event.TunnelUUID,
			RequestID:    event.ID,
			Status:       200,
			ResponseSize: len(event.Payload),
			CreatedAt:    time.Now().UTC(),
		})
		h.log.Info("webhook triage complete",
			"agent", decision.TargetAgent,
			"action", decision.Action,
			"priority", decision.Priority)
		return nil
	}

	h.tm.RecordActivity(WebhookActivityEntry{
		ID:         event.ID,
		TunnelUUID: event.Source,
		RequestID:  event.ID,
		Status:     501,
		Error:      "triage pipeline not available",
		CreatedAt:  time.Now().UTC(),
	})
	return fmt.Errorf("triage pipeline not configured")
}

func (h *DispatchHandler) handleSkill(event Event, reg *TunnelRegistration) error {
	h.tm.RecordActivity(WebhookActivityEntry{
		ID:         event.ID,
		TunnelUUID: event.TunnelUUID,
		RequestID:  event.ID,
		Status:     501,
		Error:      "direct skill dispatch is not available",
		CreatedAt:  time.Now().UTC(),
	})
	return fmt.Errorf("direct skill dispatch is not available (tunnel %q, target %q)", event.TunnelUUID, reg.TargetID)
}
