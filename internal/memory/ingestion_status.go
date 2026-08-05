package memory

import (
	"sync"
	"time"
)

// IngestionStatus tracks the current state of the memory ingestion pipeline.
type IngestionStatus int

const (
	IngestionIdle IngestionStatus = iota
	IngestionRunning
	IngestionCompleted
	IngestionFailed
)

func (s IngestionStatus) String() string {
	switch s {
	case IngestionIdle:
		return "idle"
	case IngestionRunning:
		return "running"
	case IngestionCompleted:
		return "completed"
	case IngestionFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// IngestionState is the shared state for tracking ingestion progress.
type IngestionState struct {
	mu              sync.RWMutex
	status          IngestionStatus
	queueDepth      int
	currentDocument string
	lastCompletedAt *time.Time
	lastError       string
	totalProcessed  int64
	totalFailed     int64
}

// IngestionSnapshot is a point-in-time view of ingestion state.
type IngestionSnapshot struct {
	Status          IngestionStatus `json:"status"`
	QueueDepth      int             `json:"queue_depth"`
	CurrentDocument string          `json:"current_document,omitempty"`
	LastCompletedAt string          `json:"last_completed_at,omitempty"`
	LastError       string          `json:"last_error,omitempty"`
	TotalProcessed  int64           `json:"total_processed"`
	TotalFailed     int64           `json:"total_failed"`
}

var globalIngestionState = &IngestionState{}

// GetIngestionState returns the global ingestion state tracker.
func GetIngestionState() *IngestionState {
	return globalIngestionState
}

// Enqueue increments the queue depth counter.
func (s *IngestionState) Enqueue(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queueDepth += n
}

// Dequeue decrements the queue depth counter.
func (s *IngestionState) Dequeue(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queueDepth -= n
	if s.queueDepth < 0 {
		s.queueDepth = 0
	}
}

// MarkRunning marks ingestion as running for a given document.
func (s *IngestionState) MarkRunning(docID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = IngestionRunning
	s.currentDocument = docID
}

// MarkCompleted marks ingestion as completed successfully.
func (s *IngestionState) MarkCompleted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = IngestionCompleted
	now := time.Now()
	s.lastCompletedAt = &now
	s.totalProcessed++
	s.currentDocument = ""
}

// MarkFailed marks ingestion as failed with an error message.
func (s *IngestionState) MarkFailed(err string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = IngestionFailed
	s.lastError = err
	s.totalFailed++
	s.currentDocument = ""
}

// MarkIdle resets the state to idle.
func (s *IngestionState) MarkIdle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = IngestionIdle
	s.currentDocument = ""
}

// Snapshot returns a point-in-time view of the ingestion state.
func (s *IngestionState) Snapshot() IngestionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap := IngestionSnapshot{
		Status:          s.status,
		QueueDepth:      s.queueDepth,
		CurrentDocument: s.currentDocument,
		TotalProcessed:  s.totalProcessed,
		TotalFailed:     s.totalFailed,
	}
	if s.lastCompletedAt != nil {
		snap.LastCompletedAt = s.lastCompletedAt.Format(time.RFC3339)
	}
	snap.LastError = s.lastError
	return snap
}

// QueueDepth returns the current queue depth.
func (s *IngestionState) QueueDepth() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queueDepth
}
