// Package notifications provides a notification bus with SQLite persistence,
// modeled after the Rust notifications domain. It supports multiple notification
// kinds (inbound message, mention, reminder, system alert) and delivers them
// through an event bus for UI consumption.
package notifications

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Kind categorizes a notification.
type Kind string

const (
	KindInboundMessage Kind = "inbound_message"
	KindMention        Kind = "mention"
	KindReminder       Kind = "reminder"
	KindSystemAlert    Kind = "system_alert"
	KindCronResult     Kind = "cron_result"
	KindApprovalNeeded Kind = "approval_needed"
)

// Notification is a user-visible alert.
type Notification struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	ThreadID  string    `json:"thread_id,omitempty"`
	Channel   string    `json:"channel,omitempty"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

// Handler receives notifications (e.g., for native OS notifications).
type Handler func(n Notification)

// Bus delivers notifications to subscribers and persists them.
type Bus struct {
	db   *sql.DB
	log  *slog.Logger
	mu   sync.RWMutex
	subs []Handler
}

// NewBus creates a notification bus with SQLite persistence.
func NewBus(db *sql.DB, log *slog.Logger) (*Bus, error) {
	if db == nil {
		return nil, fmt.Errorf("notification bus requires a database")
	}
	b := &Bus{db: db, log: log}
	if err := b.migrate(); err != nil {
		return nil, fmt.Errorf("notification bus migration: %w", err)
	}
	return b, nil
}

func (b *Bus) migrate() error {
	_, err := b.db.Exec(`
		CREATE TABLE IF NOT EXISTS notifications (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL DEFAULT '',
			thread_id TEXT DEFAULT '',
			channel TEXT DEFAULT '',
			read INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`)
	if err != nil {
		return err
	}
	_, err = b.db.Exec(`CREATE INDEX IF NOT EXISTS idx_notifications_unread ON notifications(read, created_at DESC)`)
	return err
}

// Subscribe registers a handler for all new notifications.
func (b *Bus) Subscribe(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, h)
}

// Notify creates, persists, and delivers a notification to all subscribers.
func (b *Bus) Notify(kind Kind, title, body, threadID, channel string) (*Notification, error) {
	n := &Notification{
		ID:        uuid.New().String(),
		Kind:      kind,
		Title:     title,
		Body:      body,
		ThreadID:  threadID,
		Channel:   channel,
		Read:      false,
		CreatedAt: time.Now().UTC(),
	}

	if err := b.persist(n); err != nil {
		return nil, err
	}

	// Deliver to subscribers (non-blocking).
	b.mu.RLock()
	subs := make([]Handler, len(b.subs))
	copy(subs, b.subs)
	b.mu.RUnlock()

	for _, h := range subs {
		go func(handler Handler) {
			defer func() {
				if r := recover(); r != nil {
					b.log.Warn("notification handler panic", "panic", r)
				}
			}()
			handler(*n)
		}(h)
	}

	return n, nil
}

// MarkRead marks a notification as read.
func (b *Bus) MarkRead(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, err := b.db.Exec(`UPDATE notifications SET read = 1 WHERE id = ?`, id)
	return err
}

// MarkAllRead marks all notifications as read.
func (b *Bus) MarkAllRead() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, err := b.db.Exec(`UPDATE notifications SET read = 1 WHERE read = 0`)
	return err
}

// ListUnread returns unread notifications, newest first.
func (b *Bus) ListUnread(limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	rows, err := b.db.Query(
		`SELECT id, kind, title, body, thread_id, channel, read, created_at
		 FROM notifications WHERE read = 0 ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanNotifications(rows)
}

// ListRecent returns all notifications, newest first.
func (b *Bus) ListRecent(limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	rows, err := b.db.Query(
		`SELECT id, kind, title, body, thread_id, channel, read, created_at
		 FROM notifications ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanNotifications(rows)
}

// UnreadCount returns the number of unread notifications.
func (b *Bus) UnreadCount() (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var count int
	err := b.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE read = 0`).Scan(&count)
	return count, err
}

// ── Internals ──────────────────────────────────────────────────────────────

func (b *Bus) persist(n *Notification) error {
	_, err := b.db.Exec(
		`INSERT INTO notifications (id, kind, title, body, thread_id, channel, read, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, string(n.Kind), n.Title, n.Body, n.ThreadID, n.Channel,
		boolToInt(n.Read), n.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func scanNotifications(rows *sql.Rows) ([]Notification, error) {
	var out []Notification
	for rows.Next() {
		var n Notification
		var kind, ca string
		var readInt int
		if err := rows.Scan(&n.ID, &kind, &n.Title, &n.Body, &n.ThreadID, &n.Channel, &readInt, &ca); err != nil {
			return nil, err
		}
		n.Kind = Kind(kind)
		n.Read = readInt != 0
		n.CreatedAt, _ = time.Parse(time.RFC3339, ca)
		out = append(out, n)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
