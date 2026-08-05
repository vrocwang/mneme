package sync

import (
	"context"
	"time"
)

// WorkspacePipeline wraps a filesystem-backed connector as a tick-capable
// pipeline for syncing local workspace content into memory.
type WorkspacePipeline struct {
	connector *FileSystemConnector
	name      string
	interval  time.Duration
	lastSync  time.Time
}

// NewWorkspacePipeline creates a workspace sync pipeline.
func NewWorkspacePipeline(dir string, extensions []string, interval time.Duration) *WorkspacePipeline {
	if interval == 0 {
		interval = 10 * time.Minute
	}
	return &WorkspacePipeline{
		connector: NewFileSystemConnector(dir, extensions),
		name:      "workspace:" + dir,
		interval:  interval,
	}
}

func (p *WorkspacePipeline) Name() string            { return p.name }
func (p *WorkspacePipeline) Kind() PipelineKind      { return KindWorkspace }
func (p *WorkspacePipeline) Interval() time.Duration { return p.interval }
func (p *WorkspacePipeline) LastSync() time.Time     { return p.lastSync }

func (p *WorkspacePipeline) Sync(ctx context.Context) ([]Item, error) {
	items, err := p.connector.Sync(ctx)
	p.lastSync = time.Now()
	return items, err
}
