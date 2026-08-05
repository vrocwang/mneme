package learning

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// RebuildScheduler runs periodic and event-driven facet rebuilds.
type RebuildScheduler struct {
	detector *StabilityDetector
	log      *slog.Logger
	interval time.Duration

	mu            sync.Mutex
	lastEvent     time.Time
	running       bool
	debounceTimer *time.Timer
	stopCh        chan struct{}
	stopped       chan struct{}
}

// RebuildInterval is the default periodic rebuild interval.
const RebuildInterval = 30 * time.Minute

// EventRebuildDelay is the debounce window after a triggering event.
const EventRebuildDelay = 60 * time.Second

// NewRebuildScheduler creates a rebuild scheduler.
func NewRebuildScheduler(detector *StabilityDetector, log *slog.Logger) *RebuildScheduler {
	return &RebuildScheduler{
		detector: detector,
		log:      log,
		interval: RebuildInterval,
		stopCh:   make(chan struct{}),
		stopped:  make(chan struct{}),
	}
}

// StartPeriodic begins the periodic rebuild loop. Runs until ctx is cancelled
// or Stop is called.
func (s *RebuildScheduler) StartPeriodic(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		close(s.stopped)
	}()

	// Skip the first immediate tick so producers have time to emit candidates.
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Wait for the first interval before running.
	select {
	case <-ctx.Done():
		return
	case <-s.stopCh:
		return
	case <-ticker.C:
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runRebuild("periodic")
		}
	}
}

// Stop signals the periodic rebuild loop to exit and waits for it to finish.
func (s *RebuildScheduler) Stop() {
	close(s.stopCh)
	<-s.stopped
}

// OnTriggerEvent is called when a rebuild-triggering event occurs.
// Uses timer-based debounce: multiple events within EventRebuildDelay collapse
// into a single rebuild, with no goroutine accumulation.
func (s *RebuildScheduler) OnTriggerEvent() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastEvent = time.Now()

	if s.debounceTimer == nil {
		s.debounceTimer = time.AfterFunc(EventRebuildDelay, func() {
			s.mu.Lock()
			elapsed := time.Since(s.lastEvent)
			s.mu.Unlock()

			if elapsed >= EventRebuildDelay {
				s.runRebuild("event")
			}
		})
	} else {
		s.debounceTimer.Reset(EventRebuildDelay)
	}
}

func (s *RebuildScheduler) runRebuild(trigger string) {
	now := time.Now()
	outcome, err := s.detector.Rebuild(now)
	if err != nil {
		s.log.Warn("learning rebuild failed", "trigger", trigger, "error", err)
		return
	}
	s.log.Info("learning rebuild complete",
		"trigger", trigger,
		"added", outcome.Added,
		"evicted", outcome.Evicted,
		"kept", outcome.Kept,
		"total", outcome.TotalSize,
	)
}
