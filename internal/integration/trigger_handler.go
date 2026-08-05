package integration

import (
	"context"
	"encoding/json"
	"time"
)

// TriggerEvent is a webhook event from a connected service, normalised
// for consumption by the trigger handling pipeline.
type TriggerEvent struct {
	// ConnectionID identifies which user connection fired the trigger.
	ConnectionID string `json:"connection_id"`
	// Toolkit is the service slug (e.g. "gmail", "slack", "github").
	Toolkit string `json:"toolkit"`
	// EventType is the provider-specific event name (e.g. "new_email", "new_message").
	EventType string `json:"event_type"`
	// Payload is the raw webhook body.
	Payload json.RawMessage `json:"payload"`
	// ReceivedAt is when the trigger was ingested.
	ReceivedAt time.Time `json:"received_at"`
	// ID is an opaque trigger identifier for deduplication.
	ID string `json:"id"`
}

// TriggerResult describes what happened after processing a trigger.
type TriggerResult struct {
	Handled      bool   `json:"handled"`
	Action       string `json:"action"` // "synced", "notified", "triaged", "ignored"
	Message      string `json:"message,omitempty"`
	AgentSpawned bool   `json:"agent_spawned,omitempty"`
}

// TriggerHandler processes webhook trigger events from connected services.
// Implementations receive normalised TriggerEvents and decide what to do:
// sync fresh data, notify the user, spawn a triage agent, or ignore.
type TriggerHandler interface {
	// ID returns the handler identifier.
	ID() string

	// SupportedToolkits returns which toolkits this handler accepts triggers for.
	SupportedToolkits() []string

	// HandleTrigger processes a single trigger event.
	HandleTrigger(ctx context.Context, event TriggerEvent) (*TriggerResult, error)

	// Deduplicate returns true if the event should be skipped (e.g. already processed).
	Deduplicate(ctx context.Context, event TriggerEvent) (bool, error)
}

// TriggerHistoryStore persists processed trigger IDs for deduplication.
type TriggerHistoryStore interface {
	// HasProcessed returns true if this trigger ID was already handled.
	HasProcessed(ctx context.Context, triggerID string) (bool, error)
	// MarkProcessed records a trigger ID as handled.
	MarkProcessed(ctx context.Context, triggerID string, result TriggerResult) error
	// Prune removes entries older than the given duration.
	Prune(ctx context.Context, olderThan time.Duration) (int64, error)
}

// NoopTriggerHistoryStore is a TriggerHistoryStore that never deduplicates.
type NoopTriggerHistoryStore struct{}

func (NoopTriggerHistoryStore) HasProcessed(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (NoopTriggerHistoryStore) MarkProcessed(_ context.Context, _ string, _ TriggerResult) error {
	return nil
}
func (NoopTriggerHistoryStore) Prune(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}
