package channels

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// ProactiveMessage is a scheduled or trigger-based message to be sent via a channel.
type ProactiveMessage struct {
	ID        string           `json:"id"`
	Channel   string           `json:"channel"`
	Target    string           `json:"target"`
	Content   string           `json:"content"`
	Trigger   ProactiveTrigger `json:"trigger"`
	CreatedAt time.Time        `json:"created_at"`
}

// ProactiveTrigger describes when a proactive message should fire.
type ProactiveTrigger struct {
	Kind      string        `json:"kind"` // "interval", "cron", "event", "idle"
	Interval  time.Duration `json:"interval,omitempty"`
	CronExpr  string        `json:"cron_expr,omitempty"`
	EventKind string        `json:"event_kind,omitempty"`
	IdleAfter time.Duration `json:"idle_after,omitempty"`
}

// ProactiveManager schedules and dispatches proactive messages across channels.
type ProactiveManager struct {
	log      *slog.Logger
	mu       sync.Mutex
	messages map[string]*ProactiveMessage
	sender   func(ctx context.Context, channel, target, content string) error
}

// NewProactiveManager creates a proactive message manager.
func NewProactiveManager(log *slog.Logger, sender func(ctx context.Context, channel, target, content string) error) *ProactiveManager {
	if log == nil {
		log = slog.Default()
	}
	return &ProactiveManager{
		log:      log,
		messages: make(map[string]*ProactiveMessage),
		sender:   sender,
	}
}

// Schedule adds a proactive message to be dispatched on its trigger.
func (pm *ProactiveManager) Schedule(msg ProactiveMessage) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.messages[msg.ID] = &msg
	pm.log.Info("proactive message scheduled", "id", msg.ID, "channel", msg.Channel, "trigger", msg.Trigger.Kind)
}

// Cancel removes a scheduled proactive message.
func (pm *ProactiveManager) Cancel(id string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.messages, id)
}

// List returns all scheduled proactive messages.
func (pm *ProactiveManager) List() []ProactiveMessage {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	result := make([]ProactiveMessage, 0, len(pm.messages))
	for _, m := range pm.messages {
		result = append(result, *m)
	}
	return result
}

// RunInterval processes interval-based proactive messages.
// Call this periodically (e.g., every minute via a cron job).
func (pm *ProactiveManager) RunInterval(ctx context.Context) {
	pm.mu.Lock()
	messages := make([]*ProactiveMessage, 0)
	for _, m := range pm.messages {
		if m.Trigger.Kind == "interval" {
			messages = append(messages, m)
		}
	}
	pm.mu.Unlock()

	for _, m := range messages {
		if pm.sender != nil {
			if err := pm.sender(ctx, m.Channel, m.Target, m.Content); err != nil {
				pm.log.Warn("proactive message send failed", "id", m.ID, "error", err)
			}
		}
	}
}

// HandleEvent dispatches event-triggered proactive messages.
func (pm *ProactiveManager) HandleEvent(ctx context.Context, eventKind string) {
	pm.mu.Lock()
	messages := make([]*ProactiveMessage, 0)
	for _, m := range pm.messages {
		if m.Trigger.Kind == "event" && m.Trigger.EventKind == eventKind {
			messages = append(messages, m)
		}
	}
	pm.mu.Unlock()

	for _, m := range messages {
		if pm.sender != nil {
			if err := pm.sender(ctx, m.Channel, m.Target, m.Content); err != nil {
				pm.log.Warn("proactive event message failed", "id", m.ID, "event", eventKind, "error", err)
			}
		}
	}
}
