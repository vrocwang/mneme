// Package autocomplete provides chat autocomplete history and suggestion
// management for the frontend composer.
package autocomplete

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Entry is a single autocomplete suggestion.
type Entry struct {
	Text     string    `json:"text"`
	Count    int       `json:"count"`
	LastUsed time.Time `json:"last_used"`
	ThreadID string    `json:"thread_id,omitempty"`
}

// Store manages autocomplete history with optional SQLite persistence.
type Store struct {
	mu      sync.RWMutex
	entries map[string]*Entry
	maxSize int
	db      *sql.DB
}

// NewStore creates an autocomplete store.
func NewStore(maxSize int) *Store {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &Store{
		entries: make(map[string]*Entry),
		maxSize: maxSize,
	}
}

// WithDB enables SQLite persistence. Call before any data operations.
func (s *Store) WithDB(db *sql.DB) *Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
	if db != nil {
		migrateAutocompleteDB(db)
		s.restoreFromDB()
	}
	return s
}

func migrateAutocompleteDB(db *sql.DB) {
	db.Exec(`CREATE TABLE IF NOT EXISTS autocomplete_entries (
		text      TEXT PRIMARY KEY,
		count     INTEGER NOT NULL DEFAULT 1,
		last_used INTEGER NOT NULL,
		thread_id TEXT NOT NULL DEFAULT ''
	)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_ac_last_used ON autocomplete_entries(last_used DESC)`)
}

func (s *Store) restoreFromDB() {
	if s.db == nil {
		return
	}
	rows, err := s.db.Query(`SELECT text, count, last_used, thread_id FROM autocomplete_entries ORDER BY last_used DESC LIMIT ?`, s.maxSize)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var e Entry
		var lastUsedMs int64
		if err := rows.Scan(&e.Text, &e.Count, &lastUsedMs, &e.ThreadID); err != nil {
			continue
		}
		e.LastUsed = time.UnixMilli(lastUsedMs)
		s.entries[normalizeKey(e.Text)] = &e
	}
}

// Record adds or updates an entry. ThreadID is optional.
func (s *Store) Record(text, threadID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := normalizeKey(text)
	if entry, ok := s.entries[key]; ok {
		entry.Count++
		entry.LastUsed = time.Now()
		if threadID != "" {
			entry.ThreadID = threadID
		}
		s.persistEntry(entry)
		return
	}

	if len(s.entries) >= s.maxSize {
		s.evictOldest()
	}

	e := &Entry{Text: text, Count: 1, LastUsed: time.Now(), ThreadID: threadID}
	s.entries[key] = e
	s.persistEntry(e)
}

// Suggest returns matching entries ordered by relevance (count × recency).
func (s *Store) Suggest(prefix string, limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	var matches []Entry
	prefixLower := strings.ToLower(strings.TrimSpace(prefix))

	for key, e := range s.entries {
		if prefixLower == "" || strings.HasPrefix(key, prefixLower) {
			matches = append(matches, *e)
		}
	}

	sortByScore(matches)

	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

// Recent returns the most recently used entries.
func (s *Store) Recent(limit int) []Entry {
	return s.Suggest("", limit)
}

// Clear removes all entries from memory and database.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]*Entry)
	if s.db != nil {
		s.db.Exec(`DELETE FROM autocomplete_entries`)
	}
}

// ── Persistence ─────────────────────────────────────────────────

func (s *Store) persistEntry(e *Entry) {
	if s.db == nil {
		return
	}
	s.db.Exec(
		`INSERT OR REPLACE INTO autocomplete_entries (text, count, last_used, thread_id) VALUES (?, ?, ?, ?)`,
		e.Text, e.Count, e.LastUsed.UnixMilli(), e.ThreadID,
	)
}

func (s *Store) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for k, e := range s.entries {
		if oldestKey == "" || e.LastUsed.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.LastUsed
		}
	}
	if oldestKey != "" {
		if s.db != nil {
			s.db.Exec(`DELETE FROM autocomplete_entries WHERE text = ?`, s.entries[oldestKey].Text)
		}
		delete(s.entries, oldestKey)
	}
}

// ── Helpers ─────────────────────────────────────────────────────

func normalizeKey(s string) string {
	key := strings.ToLower(strings.TrimSpace(s))
	if len(key) > 200 {
		key = key[:200]
	}
	return key
}

func sortByScore(entries []Entry) {
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if score(entries[j]) > score(entries[i]) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
}

func score(e Entry) float64 {
	return float64(e.Count) * recencyWeight(e.LastUsed)
}

func recencyWeight(t time.Time) float64 {
	hours := time.Since(t).Hours()
	switch {
	case hours < 1:
		return 2.0
	case hours < 24:
		return 1.5
	case hours < 168:
		return 1.0
	default:
		return 0.5
	}
}

// ── Diagnostics ─────────────────────────────────────────────────

// Stats returns entry count and oldest/newest timestamps for debugging.
func (s *Store) Stats() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("autocomplete: %d entries (max %d)", len(s.entries), s.maxSize)
}
