package main

import (
	"github.com/simon/mneme/pkg/events"
)

// approvalEventBridge implements approval.ApprovalEventPublisher by routing
// approval lifecycle events into the internal event bus, where subscribers
// (including the Wails bridge) can forward them to the frontend.
//
// busFn is a lazy getter (func() *events.Bus) so the bridge works even when
// the event bus is created after the approval gate during startup.
type approvalEventBridge struct {
	busFn func() *events.Bus
}

func (b *approvalEventBridge) PublishApprovalEvent(kind string, data map[string]interface{}) {
	bus := b.busFn()
	if bus == nil {
		return
	}
	var eventKind events.EventKind
	switch kind {
	case "requested":
		eventKind = events.KindApprovalRequested
	case "decided":
		eventKind = events.KindApprovalDecided
	default:
		eventKind = events.KindApprovalDecided
	}
	bus.PublishTyped(events.DomainApproval, eventKind, data)
}
