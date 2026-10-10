// Package sync provides connectors that feed external data sources into the memory pipeline.
package sync

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Connector feeds external data into the memory pipeline.
type Connector interface {
	// Name returns a human-readable connector identifier.
	Name() string
	// Sync performs one synchronization pass, returning ingested items.
	Sync(ctx context.Context) ([]Item, error)
}

// Item is a single piece of ingested content.
type Item struct {
	Source   string
	Path     string
	Content  string
	Modified time.Time
}

// Pipeline is the interface connectors use to feed memory.
type Pipeline interface {
	IndexContent(source, content string) error
	IndexContentWithTaint(source, content, taint string) error
}

// Manager orchestrates multiple connectors.
type Manager struct {
	log        *slog.Logger
	mu         sync.Mutex
	connectors map[string]Connector
}

// NewManager creates a connector manager.
func NewManager(log *slog.Logger) *Manager {
	return &Manager{
		log:        log,
		connectors: make(map[string]Connector),
	}
}

// Register adds a connector to the manager.
func (m *Manager) Register(c Connector) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectors[c.Name()] = c
	if m.log != nil {
		m.log.Info("registered sync connector", "name", c.Name())
	}
}

// SyncAll runs all connectors and sends results to the pipeline.
func (m *Manager) SyncAll(ctx context.Context, pipeline Pipeline) {
	m.mu.Lock()
	connectors := make([]Connector, 0, len(m.connectors))
	for _, c := range m.connectors {
		connectors = append(connectors, c)
	}
	m.mu.Unlock()

	for _, c := range connectors {
		m.syncOne(ctx, c, pipeline)
	}
}

func (m *Manager) syncOne(ctx context.Context, c Connector, pipeline Pipeline) {
	if m.log != nil {
		m.log.Debug("syncing connector", "name", c.Name())
	}
	items, err := c.Sync(ctx)
	if err != nil {
		if m.log != nil {
			m.log.Warn("connector sync failed", "name", c.Name(), "error", err)
		}
		return
	}
	for _, item := range items {
		if err := pipeline.IndexContentWithTaint(item.Source, item.Content, "external_sync"); err != nil && m.log != nil {
			m.log.Warn("sync index content failed", "source", item.Source, "error", err)
		}
	}
	if m.log != nil {
		m.log.Debug("connector sync complete", "name", c.Name(), "items", len(items))
	}
}

// ── FileSystemConnector ──────────────────────────────────────────

// ── TextConnector ────────────────────────────────────────────────

// ── Format helpers ───────────────────────────────────────────────
