package integration

import (
	"context"
	"log/slog"

	"github.com/simon/mneme/internal/memory/sync"
)

// SyncBridge adapts the integration SyncConnector interface to the existing
// memory/sync.Connector interface so that integration connectors can feed
// directly into the memory pipeline's sync manager.
type SyncBridge struct {
	source   SyncConnector
	pipeline sync.Pipeline
	log      *slog.Logger
}

// NewSyncBridge creates a bridge from an integration SyncConnector to the
// existing memory sync pipeline.
func NewSyncBridge(source SyncConnector, pipeline sync.Pipeline, log *slog.Logger) *SyncBridge {
	if log == nil {
		log = slog.Default()
	}
	return &SyncBridge{
		source:   source,
		pipeline: pipeline,
		log:      log.With("bridge", source.ID()),
	}
}

// Name returns the bridge name (delegates to source ID).
func (b *SyncBridge) Name() string {
	return b.source.ID()
}

// Sync runs the integration connector and feeds documents into the pipeline.
func (b *SyncBridge) Sync(ctx context.Context) ([]sync.Item, error) {
	docs, err := b.source.Sync(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]sync.Item, 0, len(docs))
	for _, doc := range docs {
		items = append(items, sync.Item{
			Source:   doc.Source,
			Path:     doc.Path,
			Content:  doc.Content,
			Modified: doc.Modified,
		})

		// Feed into the memory pipeline immediately.
		if b.pipeline != nil {
			b.pipeline.IndexContentWithTaint(doc.Source, doc.Content, "external_sync")
		}
	}

	b.log.Debug("sync completed", "docs", len(docs))
	return items, nil
}
