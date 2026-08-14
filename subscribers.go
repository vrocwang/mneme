package main

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/notifications"
	"github.com/simon/mneme/pkg/events"
)

// subscriberSet holds event bus subscriptions for cleanup at shutdown.
type subscriberSet struct {
	subs []*events.Subscription
}

func (s *subscriberSet) unsubscribeAll() {
	for _, sub := range s.subs {
		sub.Unsubscribe()
	}
}

// registerSubscribers wires all domain event subscribers onto the bus.
func (a *App) registerSubscribers() *subscriberSet {
	bus := a.EventBus
	set := &subscriberSet{}

	// ── Notification subscriber — creates notifications from system events ──
	// Only DomainSystem events are published today; tool/cron events are
	// audited via the eino callback path rather than the event bus.
	if a.NotifBus != nil {
		sub := bus.SubscribeDomain(func(e events.Event) {
			body := formatEventSummary(e)
			if body != "" {
				a.NotifBus.Notify(notifications.KindSystemAlert, "System", body, "", "")
			}
		}, events.DomainSystem)
		set.subs = append(set.subs, sub)
	}

	// ── Approval event subscriber — forwards to Wails frontend ─────────────
	sub := bus.SubscribeDomain(func(e events.Event) {
		data, _ := e.Data.(map[string]interface{})
		runtime.EventsEmit(a.ctx, "approval:"+string(e.Kind), map[string]interface{}{
			"id":     stringField(data, "id"),
			"tool":   stringField(data, "tool"),
			"args":   stringField(data, "args"),
			"reason": stringField(data, "reason"),
		})
	}, events.DomainApproval)
	set.subs = append(set.subs, sub)

	// ── Subagent/background task event subscriber — forwards to Wails frontend ──
	sub = bus.SubscribeDomain(func(e events.Event) {
		evt, _ := e.Data.(agent.BackgroundProgressEvent)
		runtime.EventsEmit(a.ctx, "chat:"+string(e.Kind), map[string]interface{}{
			"task_id":       evt.TaskID,
			"status":        evt.Status,
			"message":       evt.Message,
			"checkpoint_id": evt.CheckPointID,
			"token_count":   evt.TokenCount,
			"tool_calls":    evt.ToolCalls,
			"error":         evt.Error,
		})
	}, events.DomainAgent)
	set.subs = append(set.subs, sub)

	// ── Webhook event subscriber ───────────────────────────────────────────
	sub = bus.SubscribeDomain(func(e events.Event) {
		a.Log.Info("webhook event", "kind", e.Kind, "tunnel", e.Topic)
	}, events.DomainWebhook)
	set.subs = append(set.subs, sub)

	return set
}

func stringField(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	v, _ := data[key].(string)
	return v
}

// formatEventSummary returns a human-readable summary of a system event.
func formatEventSummary(e events.Event) string {
	switch e.Kind {
	case events.KindSystemStartup:
		return "Mneme started"
	case events.KindSystemShutdown:
		return "Mneme shutting down"
	default:
		return string(e.Kind)
	}
}
