package main

import (
	"fmt"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/simon/mneme/internal/desktop"
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
				if _, err := a.NotifBus.Notify(notifications.KindSystemAlert, "System", body, "", ""); err != nil {
					a.Log.Warn("notification persist failed", "kind", "system_alert", "error", err)
				}
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
			if _, err := a.NotifBus.Notify(notifications.KindApprovalNeeded,
				"Approval required",
				"Tool "+stringField(data, "tool")+" is waiting for your decision.", "", ""); err != nil {
				a.Log.Warn("notification persist failed", "kind", "approval_needed", "error", err)
			}
		}
	}, events.DomainApproval)
	set.subs = append(set.subs, sub)

	// ── Webhook event subscriber ───────────────────────────────────────────
	sub = bus.SubscribeDomain(func(e events.Event) {
		a.Log.Info("webhook event", "kind", e.Kind, "tunnel", e.Topic)
	}, events.DomainWebhook)
	set.subs = append(set.subs, sub)

	// ── Screen intelligence subscriber — forwards observations to the UI ────
	// The desktop screen-intel sink publishes each capture/vision observation
	// on the desktop domain. This bridge is what makes the result reach a
	// consumer; the frontend listener can subscribe to "screen:intel" later.
	sub = bus.SubscribeDomain(func(e events.Event) {
		if e.Kind != events.KindScreenIntel {
			return
		}
		result, ok := e.Data.(desktop.ScreenCaptureResult)
		if !ok {
			a.Log.Warn("screen intel event with unexpected payload",
				"kind", e.Kind, "type", fmt.Sprintf("%T", e.Data))
			return
		}
		if a.ctx == nil {
			return
		}
		runtime.EventsEmit(a.ctx, "screen:intel", map[string]interface{}{
			"timestamp":       result.Timestamp.Format(time.RFC3339Nano),
			"active_window":   result.ActiveWindow,
			"screenshot_path": result.ScreenshotPath,
			"summary":         result.Summary,
			"error":           result.Error,
		})
	}, events.DomainDesktop)
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
