package integration

import (
	"context"
	"time"

	"github.com/simon/mneme/internal/memory/sync"
)

// PipelineSyncOutcome captures the result of a single sync pass from the
// integration orchestrator's perspective.
type PipelineSyncOutcome struct {
	ConnectorID  string        `json:"connector_id"`
	Kind         string        `json:"kind"`
	DocsIngested int           `json:"docs_ingested"`
	DocsSkipped  int           `json:"docs_skipped"`
	Error        string        `json:"error,omitempty"`
	StartedAt    time.Time     `json:"started_at"`
	FinishedAt   time.Time     `json:"finished_at"`
	Elapsed      time.Duration `json:"elapsed_ms"`
}

// SyncPipeline is the unified interface for periodic data sync from external
// services into the memory store. Mirroring Rust's SyncPipeline trait, it
// defines three lifecycle phases: Init (one-time setup), Tick (periodic sync),
// and the ability to Repeat on a configurable interval.
type SyncPipeline interface {
	// Kind returns the pipeline category (delegates to sync.PipelineKind).
	Kind() sync.PipelineKind

	// Init performs one-time setup.
	Init(ctx context.Context) error

	// Tick performs one synchronisation pass.
	Tick(ctx context.Context, reason sync.SyncReason) PipelineSyncOutcome

	// Interval returns the recommended interval between periodic ticks.
	Interval() time.Duration

	// Enabled returns whether this pipeline is configured and enabled.
	Enabled() bool

	// Name returns a human-readable name for this pipeline.
	Name() string
}

// SyncOrchestrator manages multiple SyncPipelines, running periodic ticks
// and handling trigger-based sync requests.
type SyncOrchestrator struct {
	pipelines []SyncPipeline
}

// NewSyncOrchestrator creates an orchestrator managing the given pipelines.
func NewSyncOrchestrator(pipelines ...SyncPipeline) *SyncOrchestrator {
	return &SyncOrchestrator{pipelines: pipelines}
}

// Add registers an additional pipeline.
func (o *SyncOrchestrator) Add(p SyncPipeline) {
	o.pipelines = append(o.pipelines, p)
}

// RunTick executes one sync pass for every enabled pipeline.
func (o *SyncOrchestrator) RunTick(ctx context.Context, reason sync.SyncReason) []PipelineSyncOutcome {
	var outcomes []PipelineSyncOutcome
	for _, p := range o.pipelines {
		if !p.Enabled() {
			continue
		}
		outcomes = append(outcomes, p.Tick(ctx, reason))
	}
	return outcomes
}

// RunTickForKind runs one sync pass for the first enabled pipeline of the given kind.
func (o *SyncOrchestrator) RunTickForKind(ctx context.Context, kind sync.PipelineKind, reason sync.SyncReason) (PipelineSyncOutcome, bool) {
	for _, p := range o.pipelines {
		if p.Kind() == kind && p.Enabled() {
			return p.Tick(ctx, reason), true
		}
	}
	return PipelineSyncOutcome{}, false
}

// List returns descriptors of all registered pipelines.
func (o *SyncOrchestrator) List() []PipelineDescriptor {
	var out []PipelineDescriptor
	for _, p := range o.pipelines {
		out = append(out, PipelineDescriptor{
			Name:     p.Name(),
			Kind:     p.Kind().String(),
			Enabled:  p.Enabled(),
			Interval: p.Interval().String(),
		})
	}
	return out
}

// PipelineDescriptor is a lightweight summary of a sync pipeline.
type PipelineDescriptor struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Enabled  bool   `json:"enabled"`
	Interval string `json:"interval"`
}
