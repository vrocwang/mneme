package webhooks

import (
	"sync"
	"time"
)

// ProviderEvent is a normalized event from an external provider surface
// (Discord, Slack, Telegram, etc.) that needs the user's attention.
type ProviderEvent struct {
	Provider          string    `json:"provider"`
	AccountID         string    `json:"account_id"`
	EventKind         string    `json:"event_kind"`
	EntityID          string    `json:"entity_id,omitempty"`
	ThreadID          string    `json:"thread_id,omitempty"`
	Title             string    `json:"title,omitempty"`
	Snippet           string    `json:"snippet,omitempty"`
	SenderName        string    `json:"sender_name,omitempty"`
	SenderHandle      string    `json:"sender_handle,omitempty"`
	Timestamp         time.Time `json:"timestamp"`
	DeepLink          string    `json:"deep_link,omitempty"`
	RequiresAttention bool      `json:"requires_attention"`
	RawPayload        string    `json:"raw_payload,omitempty"`
}

// RespondQueueItem is an item in the provider respond queue.
type RespondQueueItem struct {
	Event    ProviderEvent `json:"event"`
	QueuedAt time.Time     `json:"queued_at"`
}

// ProviderSurface manages the respond queue for assistive UI surfaces.
type ProviderSurface struct {
	mu       sync.RWMutex
	queue    []RespondQueueItem
	maxItems int
}

// NewProviderSurface creates a new provider surface manager.
func NewProviderSurface() *ProviderSurface {
	return &ProviderSurface{
		maxItems: 500,
	}
}

// IngestEvent adds or updates a provider event in the respond queue.
// Deduplication is by composite key: provider + account_id + entity_id.
func (ps *ProviderSurface) IngestEvent(event ProviderEvent) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Upsert: replace existing event with same composite key.
	for i, item := range ps.queue {
		if item.Event.Provider == event.Provider &&
			item.Event.AccountID == event.AccountID &&
			item.Event.EntityID == event.EntityID {
			ps.queue[i] = RespondQueueItem{Event: event, QueuedAt: time.Now().UTC()}
			return
		}
	}

	ps.queue = append(ps.queue, RespondQueueItem{Event: event, QueuedAt: time.Now().UTC()})
	if len(ps.queue) > ps.maxItems {
		ps.queue = ps.queue[len(ps.queue)-ps.maxItems:]
	}
}

// ListQueue returns all items in the respond queue.
func (ps *ProviderSurface) ListQueue() []RespondQueueItem {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	result := make([]RespondQueueItem, len(ps.queue))
	copy(result, ps.queue)
	return result
}

// ListByProvider returns queue items filtered by provider name.
func (ps *ProviderSurface) ListByProvider(provider string) []RespondQueueItem {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	var result []RespondQueueItem
	for _, item := range ps.queue {
		if item.Event.Provider == provider {
			result = append(result, item)
		}
	}
	return result
}

// Count returns the number of items in the respond queue.
func (ps *ProviderSurface) Count() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.queue)
}

// Clear removes all items from the respond queue.
func (ps *ProviderSurface) Clear() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.queue = ps.queue[:0]
}
