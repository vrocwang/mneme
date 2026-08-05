// Package segments implements conversation segmentation matching Rust
// conversation_segments store. Groups consecutive turns into topic-coherent
// blocks with boundary detection via time gap, embedding drift, and explicit
// topic-change markers.
package segments

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/simon/mneme/internal/memory/store"
)

// Status represents the lifecycle state of a conversation segment.
type Status string

const (
	StatusOpen       Status = "open"
	StatusClosed     Status = "closed"
	StatusSummarised Status = "summarised"
)

// Segment is a topic-coherent block of conversation turns.
type Segment struct {
	SegmentID       string
	SessionID       string
	Namespace       string
	StartEpisodicID int64
	EndEpisodicID   int64
	StartTimestamp  float64
	EndTimestamp    float64
	TurnCount       int
	Summary         string
	Embedding       []float32
	TopicKeywords   string
	Status          Status
	CreatedAt       float64
	UpdatedAt       float64
	StartSeq        int
	EndSeq          int
}

// BoundaryConfig controls when a new segment is created.
type BoundaryConfig struct {
	MaxTimeGapSecs      float64 // default 600 (10 min)
	MinCosineSimilarity float64 // default 0.4
	MaxTurnsPerSegment  int     // default 20
}

// DefaultBoundaryConfig returns production-safe defaults matching Rust.
func DefaultBoundaryConfig() BoundaryConfig {
	return BoundaryConfig{
		MaxTimeGapSecs:      600.0,
		MinCosineSimilarity: 0.4,
		MaxTurnsPerSegment:  20,
	}
}

// Store persists conversation segments to SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a segment store. Tables are expected to exist (created by store.NewStore).
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// OpenSegment creates a new open segment for a session.
func (s *Store) OpenSegment(sessionID, namespace string, startEpisodicID int64, startSeq int) (*Segment, error) {
	now := float64(time.Now().UnixNano()) / 1e9
	seg := &Segment{
		SegmentID:       "seg_" + uuid.New().String()[:12],
		SessionID:       sessionID,
		Namespace:       namespace,
		StartEpisodicID: startEpisodicID,
		StartTimestamp:  now,
		TurnCount:       0,
		Status:          StatusOpen,
		CreatedAt:       now,
		UpdatedAt:       now,
		StartSeq:        startSeq,
	}
	_, err := s.db.Exec(
		`INSERT INTO conversation_segments (segment_id, session_id, namespace, start_episodic_id,
		 start_timestamp, turn_count, status, created_at, updated_at, start_seq)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seg.SegmentID, seg.SessionID, seg.Namespace, seg.StartEpisodicID,
		seg.StartTimestamp, seg.TurnCount, seg.Status, seg.CreatedAt, seg.UpdatedAt, seg.StartSeq,
	)
	return seg, err
}

// OpenSegmentForSession returns the currently open segment for a session, or nil.
func (s *Store) OpenSegmentForSession(sessionID string) (*Segment, error) {
	row := s.db.QueryRow(
		`SELECT segment_id, session_id, namespace, start_episodic_id, COALESCE(end_episodic_id,0),
		 start_timestamp, COALESCE(end_timestamp,0), turn_count, COALESCE(summary,''),
		 COALESCE(topic_keywords,''), status, created_at, updated_at,
		 COALESCE(start_seq,0), COALESCE(end_seq,0)
		 FROM conversation_segments WHERE session_id = ? AND status = 'open'
		 ORDER BY created_at DESC LIMIT 1`, sessionID,
	)
	seg := &Segment{}
	err := row.Scan(
		&seg.SegmentID, &seg.SessionID, &seg.Namespace, &seg.StartEpisodicID, &seg.EndEpisodicID,
		&seg.StartTimestamp, &seg.EndTimestamp, &seg.TurnCount, &seg.Summary,
		&seg.TopicKeywords, &seg.Status, &seg.CreatedAt, &seg.UpdatedAt,
		&seg.StartSeq, &seg.EndSeq,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return seg, nil
}

// AppendTurn increments the turn count and updates the end timestamp/episodic ID.
func (s *Store) AppendTurn(segmentID string, episodicID int64, endSeq int) error {
	now := float64(time.Now().UnixNano()) / 1e9
	_, err := s.db.Exec(
		`UPDATE conversation_segments SET turn_count = turn_count + 1,
		 end_episodic_id = ?, end_timestamp = ?, end_seq = ?, updated_at = ?
		 WHERE segment_id = ?`,
		episodicID, now, endSeq, now, segmentID,
	)
	return err
}

// CloseSegment marks a segment as closed.
func (s *Store) CloseSegment(segmentID string) error {
	now := float64(time.Now().UnixNano()) / 1e9
	_, err := s.db.Exec(
		`UPDATE conversation_segments SET status = ?, updated_at = ? WHERE segment_id = ?`,
		string(StatusClosed), now, segmentID,
	)
	return err
}

// SummarizeSegment stores the summary and marks as summarised.
func (s *Store) SummarizeSegment(segmentID, summary, keywords string, embedding []float32) error {
	now := float64(time.Now().UnixNano()) / 1e9
	_, err := s.db.Exec(
		`UPDATE conversation_segments SET summary = ?, topic_keywords = ?, status = ?, updated_at = ?
		 WHERE segment_id = ?`,
		summary, keywords, string(StatusSummarised), now, segmentID,
	)
	return err
}

// ListSegments returns all segments for a session ordered by creation time.
func (s *Store) ListSegments(sessionID string) ([]Segment, error) {
	rows, err := s.db.Query(
		`SELECT segment_id, session_id, namespace, start_episodic_id, COALESCE(end_episodic_id,0),
		 start_timestamp, COALESCE(end_timestamp,0), turn_count, COALESCE(summary,''),
		 COALESCE(topic_keywords,''), status, created_at, updated_at,
		 COALESCE(start_seq,0), COALESCE(end_seq,0)
		 FROM conversation_segments WHERE session_id = ?
		 ORDER BY created_at ASC`, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segs []Segment
	for rows.Next() {
		var seg Segment
		if err := rows.Scan(
			&seg.SegmentID, &seg.SessionID, &seg.Namespace, &seg.StartEpisodicID, &seg.EndEpisodicID,
			&seg.StartTimestamp, &seg.EndTimestamp, &seg.TurnCount, &seg.Summary,
			&seg.TopicKeywords, &seg.Status, &seg.CreatedAt, &seg.UpdatedAt,
			&seg.StartSeq, &seg.EndSeq,
		); err != nil {
			return nil, err
		}
		segs = append(segs, seg)
	}
	return segs, rows.Err()
}

// SegmentsPendingSummary returns closed segments that haven't been summarised yet.
func (s *Store) SegmentsPendingSummary(limit int) ([]Segment, error) {
	rows, err := s.db.Query(
		`SELECT segment_id, session_id, namespace, start_episodic_id, COALESCE(end_episodic_id,0),
		 start_timestamp, COALESCE(end_timestamp,0), turn_count, COALESCE(summary,''),
		 COALESCE(topic_keywords,''), status, created_at, updated_at,
		 COALESCE(start_seq,0), COALESCE(end_seq,0)
		 FROM conversation_segments WHERE status = 'closed'
		 ORDER BY created_at ASC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segs []Segment
	for rows.Next() {
		var seg Segment
		if err := rows.Scan(
			&seg.SegmentID, &seg.SessionID, &seg.Namespace, &seg.StartEpisodicID, &seg.EndEpisodicID,
			&seg.StartTimestamp, &seg.EndTimestamp, &seg.TurnCount, &seg.Summary,
			&seg.TopicKeywords, &seg.Status, &seg.CreatedAt, &seg.UpdatedAt,
			&seg.StartSeq, &seg.EndSeq,
		); err != nil {
			return nil, err
		}
		segs = append(segs, seg)
	}
	return segs, rows.Err()
}

// DetectBoundary checks whether a new segment should be created for a turn.
// Returns true if the new turn is a topic boundary relative to the current segment.
func DetectBoundary(config BoundaryConfig, turnContent string, currentSegment *Segment, turnEmbedding []float32, segmentEmbedding []float32, lastTurnTime float64) bool {
	// 1. Turn count exceeded.
	if currentSegment != nil && currentSegment.TurnCount >= config.MaxTurnsPerSegment {
		return true
	}

	// 2. Time gap exceeded.
	if currentSegment != nil && lastTurnTime > 0 {
		gap := lastTurnTime - currentSegment.StartTimestamp
		if gap > config.MaxTimeGapSecs {
			return true
		}
	}

	// 3. Explicit topic-change markers.
	if hasTopicChangeMarker(turnContent) {
		return true
	}

	// 4. Embedding drift.
	if len(turnEmbedding) > 0 && len(segmentEmbedding) > 0 && len(turnEmbedding) == len(segmentEmbedding) {
		sim := cosineSimilarity(turnEmbedding, segmentEmbedding)
		if sim < config.MinCosineSimilarity {
			return true
		}
	}

	return false
}

var topicChangeMarkers = []string{
	"now let's", "switching to", "different topic", "by the way,",
	"moving on", "let's talk about", "changing subjects", "on another note",
	"let's switch", "that reminds me", "speaking of which",
	"new topic:", "next:", "separately,", "unrelated,",
}

func hasTopicChangeMarker(content string) bool {
	lower := strings.ToLower(content)
	for _, m := range topicChangeMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// cosineSimilarity delegates to store.CosineSimilarity, which clamps
// results to [0, 1] for semantically meaningful scores.
func cosineSimilarity(a, b []float32) float64 {
	return store.CosineSimilarity(a, b)
}

// IncrementalMeanEmbedding updates a running mean embedding with a new vector.
// c = c + (n - c) / (count + 1) element-wise.
func IncrementalMeanEmbedding(currentCentroid, newEmbedding []float32, count int) []float32 {
	if len(currentCentroid) != len(newEmbedding) {
		return newEmbedding
	}
	result := make([]float32, len(currentCentroid))
	for i := range currentCentroid {
		result[i] = currentCentroid[i] + (newEmbedding[i]-currentCentroid[i])/float32(count+1)
	}
	return result
}

// FormatSegmentList returns a human-readable segment summary.
func FormatSegmentList(segments []Segment) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Conversation segments (%d):\n", len(segments)))
	for _, seg := range segments {
		b.WriteString(fmt.Sprintf("  [%s] %d turns", seg.Status, seg.TurnCount))
		if seg.Summary != "" {
			b.WriteString(fmt.Sprintf(" — %s", truncate(seg.Summary, 80)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
