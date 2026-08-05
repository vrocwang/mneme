package sync

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// RunStatus tracks the lifecycle state of a sync pipeline run.
type RunStatus string

const (
	StatusIdle    RunStatus = "idle"
	StatusRunning RunStatus = "running"
	StatusError   RunStatus = "error"
)

// SyncRecord captures the outcome of a single sync run for a pipeline.
type SyncRecord struct {
	Pipeline     string        `json:"pipeline"`
	Status       RunStatus     `json:"status"`
	ItemsFetched int           `json:"items_fetched"`
	ItemsNew     int           `json:"items_new"`
	Error        string        `json:"error,omitempty"`
	StartedAt    time.Time     `json:"started_at"`
	FinishedAt   time.Time     `json:"finished_at"`
	Elapsed      time.Duration `json:"elapsed_ms"`
}

// TickPipeline extends Connector with scheduling metadata needed by the
// periodic scheduler. Pipelines that don't need periodic ticks can just
// implement Connector.
type TickPipeline interface {
	Connector

	// Kind returns the pipeline category.
	Kind() PipelineKind

	// Interval returns the recommended tick interval. Zero = trigger/manual only.
	Interval() time.Duration

	// LastSync returns the timestamp of the most recent sync.
	LastSync() time.Time
}

// Scheduler runs sync pipelines on a periodic tick. It is designed to be
// driven by the application heartbeat (or cron) so that sync cadence stays
// coupled to the app lifecycle rather than spawning its own background
// goroutines.
type Scheduler struct {
	mu         sync.Mutex
	pipelines  []TickPipeline
	records    []SyncRecord
	maxRecords int
	log        *slog.Logger
}

// NewScheduler creates a sync scheduler.
func NewScheduler(log *slog.Logger) *Scheduler {
	return &Scheduler{
		log:        log,
		maxRecords: 50,
	}
}

// Register adds a tick-capable pipeline to the scheduler.
func (s *Scheduler) Register(p TickPipeline) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pipelines = append(s.pipelines, p)
	if s.log != nil {
		s.log.Info("sync scheduler: registered pipeline",
			"name", p.Name(), "kind", p.Kind(), "interval", p.Interval())
	}
}

// Tick runs one sync pass for all pipelines whose interval has elapsed since
// their last sync. Designed to be called from the heartbeat loop.
func (s *Scheduler) Tick(ctx context.Context) []SyncRecord {
	s.mu.Lock()
	pipelines := make([]TickPipeline, len(s.pipelines))
	copy(pipelines, s.pipelines)
	s.mu.Unlock()

	var records []SyncRecord
	for _, p := range pipelines {
		if !s.shouldTick(p) {
			continue
		}
		rec := s.runOne(ctx, p)
		s.appendRecord(rec)
		records = append(records, rec)
	}
	return records
}

// TickAll runs all pipelines regardless of interval. Used for manual sync.
func (s *Scheduler) TickAll(ctx context.Context) []SyncRecord {
	s.mu.Lock()
	pipelines := make([]TickPipeline, len(s.pipelines))
	copy(pipelines, s.pipelines)
	s.mu.Unlock()

	var records []SyncRecord
	for _, p := range pipelines {
		rec := s.runOne(ctx, p)
		s.appendRecord(rec)
		records = append(records, rec)
	}
	return records
}

// TickKind runs all pipelines of a given kind.
func (s *Scheduler) TickKind(ctx context.Context, kind PipelineKind) []SyncRecord {
	s.mu.Lock()
	var pipelines []TickPipeline
	for _, p := range s.pipelines {
		if p.Kind() == kind {
			pipelines = append(pipelines, p)
		}
	}
	s.mu.Unlock()

	var records []SyncRecord
	for _, p := range pipelines {
		rec := s.runOne(ctx, p)
		s.appendRecord(rec)
		records = append(records, rec)
	}
	return records
}

// Records returns recent sync records, newest first.
func (s *Scheduler) Records() []SyncRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SyncRecord, len(s.records))
	copy(out, s.records)
	// Reverse: newest first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (s *Scheduler) shouldTick(p TickPipeline) bool {
	interval := p.Interval()
	if interval == 0 {
		return false // trigger/manual only
	}
	last := p.LastSync()
	return time.Since(last) >= interval
}

func (s *Scheduler) runOne(ctx context.Context, p TickPipeline) SyncRecord {
	start := time.Now()
	rec := SyncRecord{
		Pipeline:  p.Name(),
		Status:    StatusRunning,
		StartedAt: start,
	}

	items, err := p.Sync(ctx)
	rec.FinishedAt = time.Now()
	rec.Elapsed = rec.FinishedAt.Sub(start)

	if err != nil {
		rec.Status = StatusError
		rec.Error = err.Error()
		if s.log != nil {
			s.log.Warn("sync scheduler: pipeline failed",
				"name", p.Name(), "error", err, "elapsed", rec.Elapsed)
		}
	} else {
		rec.Status = StatusIdle
		rec.ItemsFetched = len(items)
		if s.log != nil {
			s.log.Debug("sync scheduler: pipeline complete",
				"name", p.Name(), "items", len(items), "elapsed", rec.Elapsed)
		}
	}
	return rec
}

func (s *Scheduler) appendRecord(rec SyncRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
	if len(s.records) > s.maxRecords {
		s.records = s.records[len(s.records)-s.maxRecords:]
	}
}
