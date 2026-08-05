package main

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/simon/mneme/internal/notifications"
	"github.com/simon/mneme/internal/security"
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

	// ── Tool audit subscriber — logs tool executions to audit logger ───────
	if a.AuditLogger != nil {
		sub := bus.SubscribeDomain(func(e events.Event) {
			a.AuditLogger.Record(security.AuditToolExecution, security.AuditEvent{
				ToolName: string(e.Kind),
				Reason:   formatEventSummary(e),
			})
		}, events.DomainTool)
		set.subs = append(set.subs, sub)
	}

	// ── Notification subscriber — creates notifications from system events ──
	if a.NotifBus != nil {
		sub := bus.SubscribeDomain(func(e events.Event) {
			body := formatEventSummary(e)
			if body != "" {
				a.NotifBus.Notify(notifications.KindSystemAlert, "System", body, "", "")
			}
		}, events.DomainSystem, events.DomainCron, events.DomainTool)
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

	// ── Cron event subscriber ──────────────────────────────────────────────
	sub = bus.SubscribeDomain(func(e events.Event) {
		a.Log.Info("cron event", "kind", e.Kind, "topic", e.Topic)
	}, events.DomainCron)
	set.subs = append(set.subs, sub)

	// ── Channel inbound subscriber ─────────────────────────────────────────
	sub = bus.SubscribeDomain(func(e events.Event) {
		a.Log.Info("channel event", "kind", e.Kind, "topic", e.Topic)
	}, events.DomainChannel)
	set.subs = append(set.subs, sub)

	// ── Tool execution debug subscriber ────────────────────────────────────
	sub = bus.SubscribeDomain(func(e events.Event) {
		a.Log.Debug("tool event", "kind", e.Kind)
	}, events.DomainTool)
	set.subs = append(set.subs, sub)

	// ── Memory event debug subscriber ──────────────────────────────────────
	sub = bus.SubscribeDomain(func(e events.Event) {
		a.Log.Debug("memory event", "kind", e.Kind)
	}, events.DomainMemory)
	set.subs = append(set.subs, sub)

	// ── Subagent event subscriber — forwards to Wails frontend ──────────────
	sub = bus.SubscribeDomain(func(e events.Event) {
		data, _ := e.Data.(map[string]interface{})
		runtime.EventsEmit(a.ctx, "chat:"+string(e.Kind), map[string]interface{}{
			"agent_id":   stringField(data, "agent_id"),
			"task":       stringField(data, "task"),
			"session_id": stringField(data, "session_id"),
			"agent_type": stringField(data, "agent_type"),
		})
	}, events.DomainAgent)
	set.subs = append(set.subs, sub)

	// ── Webhook event subscriber ───────────────────────────────────────────
	sub = bus.SubscribeDomain(func(e events.Event) {
		a.Log.Info("webhook event", "kind", e.Kind, "tunnel", e.Topic)
	}, events.DomainWebhook)
	set.subs = append(set.subs, sub)

	// ── Voice event subscriber ─────────────────────────────────────────────
	sub = bus.SubscribeDomain(func(e events.Event) {
		a.Log.Debug("voice event", "kind", e.Kind)
	}, events.DomainVoice)
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

// formatEventSummary returns a human-readable summary of an event type.
func formatEventSummary(e events.Event) string {
	switch e.Kind {
	case events.KindSystemStartup:
		return "Mneme started"
	case events.KindSystemShutdown:
		return "Mneme shutting down"
	case events.KindCronTriggered:
		return "Cron job triggered"
	case events.KindCronCompleted:
		return "Cron job completed"
	case events.KindCronFailed:
		return "Cron job failed"
	case events.KindToolExecutionStarted:
		return "Tool execution started: " + e.Topic
	case events.KindToolExecutionCompleted:
		return "Tool execution completed: " + e.Topic
	case events.KindToolExecutionFailed:
		return "Tool execution failed: " + e.Topic
	case events.KindToolPolicyBlocked:
		return "Tool blocked: " + e.Topic
	case events.KindMemoryStored:
		return "Memory stored"
	case events.KindMemoryRecalled:
		return "Memory recalled"
	default:
		return string(e.Kind)
	}
}
