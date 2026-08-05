// Package events implements event extraction from conversations matching Rust
// event_log store. Extracts structured events (facts, decisions, commitments,
// preferences, questions, foresight) from conversation segments via heuristic
// regex (Tier A) and optionally LLM (Tier B) extraction.
package events

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EventType categorizes extracted events.
type EventType string

const (
	EventFact       EventType = "fact"
	EventDecision   EventType = "decision"
	EventCommitment EventType = "commitment"
	EventPreference EventType = "preference"
	EventQuestion   EventType = "question"
	EventForesight  EventType = "foresight"
)

// Event is a structured fact extracted from conversation content.
type Event struct {
	EventID       string    `json:"event_id"`
	SegmentID     string    `json:"segment_id"`
	SessionID     string    `json:"session_id"`
	Namespace     string    `json:"namespace"`
	EventType     EventType `json:"event_type"`
	Content       string    `json:"content"`
	Subject       string    `json:"subject"`
	TimestampRef  string    `json:"timestamp_ref"`
	Confidence    float64   `json:"confidence"`
	Embedding     []float32 `json:"-"`
	SourceTurnIDs string    `json:"source_turn_ids"`
	CreatedAt     float64   `json:"created_at"`
}

// Store persists extracted events to SQLite with FTS5 indexing.
type Store struct {
	db *sql.DB
}

// NewStore creates an event store (tables expected to exist).
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Insert stores an event. The FTS5 index is kept in sync by triggers.
func (s *Store) Insert(ev *Event) error {
	now := float64(time.Now().UnixNano()) / 1e9
	if ev.EventID == "" {
		ev.EventID = "evt_" + uuid.New().String()[:12]
	}
	ev.CreatedAt = now
	if ev.Namespace == "" {
		ev.Namespace = "global"
	}
	_, err := s.db.Exec(
		`INSERT INTO event_log (event_id, segment_id, session_id, namespace, event_type,
		 content, subject, timestamp_ref, confidence, source_turn_ids, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.EventID, ev.SegmentID, ev.SessionID, ev.Namespace, string(ev.EventType),
		ev.Content, ev.Subject, ev.TimestampRef, ev.Confidence, ev.SourceTurnIDs, ev.CreatedAt,
	)
	return err
}

// SearchFTS performs full-text search over events within a namespace.
func (s *Store) SearchFTS(namespace, query string, limit int) ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT e.event_id, e.segment_id, e.session_id, e.namespace, e.event_type,
		 e.content, COALESCE(e.subject,''), COALESCE(e.timestamp_ref,''), e.confidence,
		 COALESCE(e.source_turn_ids,''), e.created_at
		 FROM event_log e JOIN event_fts f ON e.rowid = f.rowid
		 WHERE e.namespace = ? AND event_fts MATCH ?
		 ORDER BY rank LIMIT ?`,
		namespace, sanitizeFTSQuery(query), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// EventsForSegment returns all events for a segment.
func (s *Store) EventsForSegment(segmentID string) ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT event_id, segment_id, session_id, namespace, event_type,
		 content, COALESCE(subject,''), COALESCE(timestamp_ref,''), confidence,
		 COALESCE(source_turn_ids,''), created_at
		 FROM event_log WHERE segment_id = ? ORDER BY created_at`, segmentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// EventsByType returns events of a specific type within a namespace.
func (s *Store) EventsByType(namespace string, eventType EventType, limit int) ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT event_id, segment_id, session_id, namespace, event_type,
		 content, COALESCE(subject,''), COALESCE(timestamp_ref,''), confidence,
		 COALESCE(source_turn_ids,''), created_at
		 FROM event_log WHERE namespace = ? AND event_type = ?
		 ORDER BY created_at DESC LIMIT ?`, namespace, string(eventType), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// ── Tier A: Heuristic extraction ──────────────────────────────────────────

// ExtractEventsHeuristic extracts events from text using regex patterns.
// Always runs; returns events with 0.6-0.8 confidence.
func ExtractEventsHeuristic(text, segmentID, sessionID string) []Event {
	sentences := splitSentences(text)
	var events []Event

	for _, sent := range sentences {
		sent = strings.TrimSpace(sent)
		if len(sent) < 10 {
			continue
		}
		lower := strings.ToLower(sent)

		// Decision patterns.
		for _, p := range decisionPatterns {
			if strings.Contains(lower, p) {
				events = append(events, Event{
					SegmentID:  segmentID,
					SessionID:  sessionID,
					EventType:  EventDecision,
					Content:    sent,
					Confidence: 0.75,
				})
				break
			}
		}

		// Commitment patterns.
		for _, p := range commitmentPatterns {
			if strings.Contains(lower, p) {
				events = append(events, Event{
					SegmentID:  segmentID,
					SessionID:  sessionID,
					EventType:  EventCommitment,
					Content:    sent,
					Confidence: 0.7,
				})
				break
			}
		}

		// Preference patterns.
		for _, p := range preferencePatterns {
			if strings.Contains(lower, p) {
				events = append(events, Event{
					SegmentID:  segmentID,
					SessionID:  sessionID,
					EventType:  EventPreference,
					Content:    sent,
					Confidence: 0.7,
				})
				break
			}
		}

		// Fact patterns.
		for _, p := range factPatterns {
			if strings.Contains(lower, p) {
				subj := extractSubject(sent)
				events = append(events, Event{
					SegmentID:  segmentID,
					SessionID:  sessionID,
					EventType:  EventFact,
					Content:    sent,
					Subject:    subj,
					Confidence: 0.65,
				})
				break
			}
		}
	}

	// Deduplicate by content.
	seen := make(map[string]bool)
	var unique []Event
	for _, e := range events {
		if !seen[e.Content] {
			seen[e.Content] = true
			unique = append(unique, e)
		}
	}
	return unique
}

var decisionPatterns = []string{
	"let's go with", "i've decided", "we decided", "going with",
	"i'll go with", "let's use", "the decision is", "we'll use",
	"let's pick", "i'll take", "we should go with",
}

var commitmentPatterns = []string{
	"by friday", "deadline is", "i will", "i commit", "plan to",
	"i'll do", "i will do", "next steps:", "action item",
	"assigned to", "responsible for", "i'll handle",
}

var preferencePatterns = []string{
	"i prefer", "i like", "i hate", "my favorite", "i don't like",
	"i love", "i enjoy", "prefer to", "i'd rather",
}

var factPatterns = []string{
	"i'm based in", "i live in", "i work at", "my name is",
	"i am a", "i'm a", "i use", "my role is", "i work as",
	"my team is", "i report to", "i'm responsible for",
}

func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder
	for _, r := range text {
		current.WriteRune(r)
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			s := strings.TrimSpace(current.String())
			if s != "" {
				sentences = append(sentences, s)
			}
			current.Reset()
		}
	}
	if current.Len() > 0 {
		sentences = append(sentences, strings.TrimSpace(current.String()))
	}
	return sentences
}

func extractSubject(sent string) string {
	lower := strings.ToLower(sent)
	for _, p := range factPatterns {
		if idx := strings.Index(lower, p); idx >= 0 {
			return strings.TrimSpace(sent[:idx+len(p)])
		}
	}
	return ""
}

func sanitizeFTSQuery(q string) string {
	var cleaned strings.Builder
	for _, r := range q {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == ' ':
			cleaned.WriteRune(r)
		}
	}
	words := strings.Fields(cleaned.String())
	if len(words) == 0 {
		return `""`
	}
	escaped := make([]string, len(words))
	for i, w := range words {
		escaped[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	return strings.Join(escaped, " ")
}

// FormatEventList returns a human-readable event summary.
func FormatEventList(events []Event) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Extracted events (%d):\n", len(events)))
	for _, ev := range events {
		b.WriteString(fmt.Sprintf("  [%s] %s (%.0f%%)\n", ev.EventType, truncateStr(ev.Content, 100), ev.Confidence*100))
	}
	return b.String()
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	var events []Event
	for rows.Next() {
		var ev Event
		if err := rows.Scan(
			&ev.EventID, &ev.SegmentID, &ev.SessionID, &ev.Namespace, &ev.EventType,
			&ev.Content, &ev.Subject, &ev.TimestampRef, &ev.Confidence,
			&ev.SourceTurnIDs, &ev.CreatedAt,
		); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}
