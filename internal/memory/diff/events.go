// Package diff provides lightweight memory change tracking via sync events.
// Unlike Rust's full snapshot→diff→checkpoint system (2,161 lines), this
// implements a simple event log: each sync completion records what changed,
// and an agent tool queries recent changes on demand.
package diff

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SyncEvent records a completed memory source sync.
type SyncEvent struct {
	ID         string    `json:"id"`
	SourceID   string    `json:"source_id"`
	SourceKind string    `json:"source_kind"` // "github", "rss", "gmail", etc.
	Timestamp  time.Time `json:"timestamp"`
	Added      int       `json:"added"`
	Updated    int       `json:"updated"`
	Removed    int       `json:"removed"`
	ItemIDs    []string  `json:"item_ids,omitempty"` // changed item identifiers
}

// Store persists sync events in SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a sync event store. Auto-creates the table.
func NewStore(db *sql.DB) (*Store, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS mem_sync_events (
		id          TEXT PRIMARY KEY,
		source_id   TEXT NOT NULL,
		source_kind TEXT NOT NULL,
		timestamp   INTEGER NOT NULL,
		added       INTEGER NOT NULL DEFAULT 0,
		updated     INTEGER NOT NULL DEFAULT 0,
		removed     INTEGER NOT NULL DEFAULT 0,
		item_ids    TEXT DEFAULT '[]'
	)`)
	if err != nil {
		return nil, fmt.Errorf("diff store: create table: %w", err)
	}
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sync_events_ts ON mem_sync_events(timestamp)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sync_events_source ON mem_sync_events(source_id)`)
	return &Store{db: db}, nil
}

// Record persists a sync event.
func (s *Store) Record(ctx context.Context, evt SyncEvent) error {
	if evt.ID == "" {
		evt.ID = fmt.Sprintf("%s-%d", evt.SourceID, evt.Timestamp.Unix())
	}
	ids, err := json.Marshal(evt.ItemIDs)
	if err != nil {
		return fmt.Errorf("marshal item IDs: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO mem_sync_events (id, source_id, source_kind, timestamp, added, updated, removed, item_ids)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		evt.ID, evt.SourceID, evt.SourceKind, evt.Timestamp.Unix(),
		evt.Added, evt.Updated, evt.Removed, string(ids))
	return err
}

// Recent returns sync events since the given duration.
func (s *Store) Recent(ctx context.Context, since time.Duration, limit int) ([]SyncEvent, error) {
	cutoff := time.Now().Add(-since).Unix()
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, source_id, source_kind, timestamp, added, updated, removed, item_ids
		 FROM mem_sync_events WHERE timestamp > ? ORDER BY timestamp DESC LIMIT ?`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []SyncEvent
	for rows.Next() {
		var e SyncEvent
		var ts int64
		var ids string
		if err := rows.Scan(&e.ID, &e.SourceID, &e.SourceKind, &ts, &e.Added, &e.Updated, &e.Removed, &ids); err != nil {
			continue
		}
		e.Timestamp = time.Unix(ts, 0)
		json.Unmarshal([]byte(ids), &e.ItemIDs)
		events = append(events, e)
	}
	return events, nil
}

// SinceLast returns all events after the given timestamp.
func (s *Store) SinceLast(ctx context.Context, lastCheck time.Time, limit int) ([]SyncEvent, error) {
	return s.Recent(ctx, time.Since(lastCheck), limit)
}

// FormatMarkdown returns events as a human-readable markdown summary.
func FormatMarkdown(events []SyncEvent) string {
	if len(events) == 0 {
		return "No memory changes detected."
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Memory Changes (%d events)\n\n", len(events)))
	for _, e := range events {
		b.WriteString(fmt.Sprintf("- **%s** (%s): ", e.SourceID, e.SourceKind))
		parts := []string{}
		if e.Added > 0 {
			parts = append(parts, fmt.Sprintf("+%d added", e.Added))
		}
		if e.Updated > 0 {
			parts = append(parts, fmt.Sprintf("~%d updated", e.Updated))
		}
		if e.Removed > 0 {
			parts = append(parts, fmt.Sprintf("-%d removed", e.Removed))
		}
		b.WriteString(strings.Join(parts, ", "))
		b.WriteString(fmt.Sprintf(" — %s\n", e.Timestamp.Format("2006-01-02 15:04")))
	}
	return b.String()
}
