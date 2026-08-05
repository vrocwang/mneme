package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// TaskSourceKind categorizes the origin of task sources.
type TaskSourceKind string

const (
	TaskSourceCron    TaskSourceKind = "cron"
	TaskSourceWebhook TaskSourceKind = "webhook"
	TaskSourceManual  TaskSourceKind = "manual"
	TaskSourceChannel TaskSourceKind = "channel"
)

// TaskSourceEntry represents a configured task source.
type TaskSourceEntry struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Kind        TaskSourceKind `json:"kind"`
	Config      string         `json:"config"` // JSON-encoded source-specific config
	Enabled     bool           `json:"enabled"`
	TargetAgent string         `json:"target_agent"`
	LastSyncAt  *time.Time     `json:"last_sync_at,omitempty"`
	NextSyncAt  *time.Time     `json:"next_sync_at,omitempty"`
	ErrorCount  int            `json:"error_count"`
	LastError   string         `json:"last_error,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// TaskSourceStore persists task source configurations.
type TaskSourceStore struct {
	mu      sync.RWMutex
	sources map[string]*TaskSourceEntry
	log     *slog.Logger
}

// NewTaskSourceStore creates a task source store.
func NewTaskSourceStore() *TaskSourceStore {
	return &TaskSourceStore{
		sources: make(map[string]*TaskSourceEntry),
		log:     slog.Default().With("component", "task-sources"),
	}
}

// Add creates a new task source.
func (s *TaskSourceStore) Add(entry *TaskSourceEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry.ID == "" {
		entry.ID = fmt.Sprintf("ts_%d", time.Now().UnixNano())
	}
	entry.CreatedAt = time.Now().UTC()
	entry.UpdatedAt = entry.CreatedAt
	s.sources[entry.ID] = entry
	return nil
}

// Get returns a task source by ID.
func (s *TaskSourceStore) Get(id string) (*TaskSourceEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.sources[id]
	return e, ok
}

// ListEnabled returns all enabled task sources.
func (s *TaskSourceStore) ListEnabled() []*TaskSourceEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*TaskSourceEntry
	for _, e := range s.sources {
		if e.Enabled {
			result = append(result, e)
		}
	}
	return result
}

// ListByKind returns task sources of a specific kind.
func (s *TaskSourceStore) ListByKind(kind TaskSourceKind) []*TaskSourceEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*TaskSourceEntry
	for _, e := range s.sources {
		if e.Kind == kind {
			result = append(result, e)
		}
	}
	return result
}

// Update modifies an existing task source.
func (s *TaskSourceStore) Update(id string, update func(*TaskSourceEntry)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.sources[id]
	if !ok {
		return fmt.Errorf("task source %q not found", id)
	}
	update(e)
	e.UpdatedAt = time.Now().UTC()
	return nil
}

// Delete removes a task source.
func (s *TaskSourceStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sources[id]; !ok {
		return fmt.Errorf("task source %q not found", id)
	}
	delete(s.sources, id)
	return nil
}

// RecordSync updates sync timestamps.
func (s *TaskSourceStore) RecordSync(id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.sources[id]
	if !ok {
		return
	}
	now := time.Now().UTC()
	e.LastSyncAt = &now

	if err != nil {
		e.ErrorCount++
		e.LastError = err.Error()
	} else {
		e.LastError = ""
	}
}

// ── Periodic sync ────────────────────────────────────────────────────

// TaskSourceSyncer runs periodic checks on task sources and triggers
// agent dispatch for sources that are due.
type TaskSourceSyncer struct {
	store        *TaskSourceStore
	dispatcher   *TaskDispatcher
	pollInterval time.Duration
	log          *slog.Logger
}

// NewTaskSourceSyncer creates a task source syncer.
func NewTaskSourceSyncer(store *TaskSourceStore, dispatcher *TaskDispatcher) *TaskSourceSyncer {
	return &TaskSourceSyncer{
		store:        store,
		dispatcher:   dispatcher,
		pollInterval: 60 * time.Second,
		log:          slog.Default().With("component", "task-source-syncer"),
	}
}

// Start begins periodic syncing.
func (s *TaskSourceSyncer) Start(ctx context.Context) {
	s.log.Info("task source syncer started", "interval", s.pollInterval)
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sync(ctx)
		}
	}
}

func (s *TaskSourceSyncer) sync(ctx context.Context) {
	for _, source := range s.store.ListEnabled() {
		// Skip sources that are not yet due.
		if source.NextSyncAt != nil && time.Now().UTC().Before(*source.NextSyncAt) {
			continue
		}

		s.log.Debug("syncing task source", "id", source.ID, "kind", source.Kind)

		// Dispatch a task to the target agent. The kind determines the prompt
		// template; cron sources are periodic checks, webhook sources are triggered.
		var err error
		switch source.Kind {
		case TaskSourceCron:
			err = s.dispatch(ctx, source, "Run periodic check: "+source.Name+" (config: "+source.Config+")")
		case TaskSourceWebhook:
			err = s.dispatch(ctx, source, "Process incoming event from "+source.Name+" (config: "+source.Config+")")
		case TaskSourceChannel:
			err = s.dispatch(ctx, source, "Handle channel message from "+source.Name)
		case TaskSourceManual:
			// Manual sources are triggered by user action, not periodic sync.
			continue
		default:
			err = s.dispatch(ctx, source, source.Name+" sync tick")
		}

		// Schedule next sync. Default: 600s (10 min), matching Rust.
		next := time.Now().UTC().Add(600 * time.Second)
		s.store.RecordSync(source.ID, err)
		s.store.mu.Lock()
		source.NextSyncAt = &next
		s.store.mu.Unlock()
	}
}

func (s *TaskSourceSyncer) dispatch(ctx context.Context, source *TaskSourceEntry, prompt string) error {
	if s.dispatcher == nil {
		return fmt.Errorf("no dispatcher configured")
	}
	task := &DispatchTask{
		AgentID:  source.TargetAgent,
		Prompt:   prompt,
		Priority: "low",
	}
	return s.dispatcher.Enqueue(task)
}
