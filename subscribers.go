package main

import (
	"time"

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

		// ── Notification delivery to the UI ────────────────────────────────
		// The bus persists notifications; without this subscriber nothing ever
		// delivered them, so every notification was write-only. Each new
		// notification is forwarded to the frontend, which renders it as a
		// toast and can fetch history through NotificationsRPC.
		a.NotifBus.Subscribe(func(n notifications.Notification) {
			if a.ctx == nil {
				return
			}
			runtime.EventsEmit(a.ctx, "notification:new", map[string]interface{}{
				"id":         n.ID,
				"kind":       string(n.Kind),
				"title":      n.Title,
				"body":       n.Body,
				"thread_id":  n.ThreadID,
				"channel":    n.Channel,
				"created_at": n.CreatedAt.Format(time.RFC3339),
			})
		})
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
		// Surface pending approvals through the notification bus too, so the
		// toast layer shows them even when the approvals view is not open.
		if e.Kind == events.KindApprovalRequested && a.NotifBus != nil {
			a.NotifBus.Notify(notifications.KindApprovalNeeded,
				"Approval required",
				"Tool "+stringField(data, "tool")+" is waiting for your decision.", "", "")
		}
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
