// Package sync provides connectors that feed external data sources into the memory pipeline.
package sync

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// FileSystemConnector watches a directory for new/changed files and ingests
// their content into the memory pipeline.
type FileSystemConnector struct {
	mu         sync.Mutex
	dir        string
	extensions []string // e.g., [".md", ".txt", ".json"]
	lastSync   map[string]time.Time
}

// NewFileSystemConnector creates a file system watcher connector.
func NewFileSystemConnector(dir string, extensions []string) *FileSystemConnector {
	return &FileSystemConnector{
		dir:        dir,
		extensions: extensions,
		lastSync:   make(map[string]time.Time),
	}
}

func (c *FileSystemConnector) Name() string {
	return "filesystem:" + c.dir
}

func (c *FileSystemConnector) Sync(ctx context.Context) ([]Item, error) {
	var items []Item
	seenPaths := make(map[string]bool)

	err := filepath.Walk(c.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable files
		}
		if info.IsDir() {
			// Skip hidden directories
			if strings.HasPrefix(info.Name(), ".") && info.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Size() > 10*1024*1024 { // 10MB limit
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !c.shouldIngest(ext) {
			return nil
		}

		modTime := info.ModTime()
		c.mu.Lock()
		last, ok := c.lastSync[path]
		c.mu.Unlock()
		if ok && !modTime.After(last) {
			seenPaths[path] = true
			return nil // unchanged
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		c.mu.Lock()
		c.lastSync[path] = modTime
		c.mu.Unlock()
		seenPaths[path] = true
		items = append(items, Item{
			Source:   "filesystem",
			Path:     path,
			Content:  string(data),
			Modified: modTime,
		})
		return nil
	})

	// Prune deleted files from lastSync.
	c.mu.Lock()
	for path := range c.lastSync {
		if !seenPaths[path] {
			delete(c.lastSync, path)
		}
	}
	c.mu.Unlock()

	return items, err
}

func (c *FileSystemConnector) shouldIngest(ext string) bool {
	if len(c.extensions) == 0 {
		return true // ingest everything
	}
	for _, e := range c.extensions {
		if e == ext {
			return true
		}
	}
	return false
}

// ── TextConnector ────────────────────────────────────────────────

// TextConnector ingests a single named text source (e.g., a manual note or external document).
type TextConnector struct {
	mu      sync.Mutex
	name    string
	content string
	synced  bool
}

// NewTextConnector creates a one-shot text source connector.
func NewTextConnector(name, content string) *TextConnector {
	return &TextConnector{name: name, content: content}
}

func (c *TextConnector) Name() string {
	return "text:" + c.name
}

func (c *TextConnector) Sync(ctx context.Context) ([]Item, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.synced {
		return nil, nil
	}
	c.synced = true
	return []Item{{
		Source:  "text:" + c.name,
		Path:    c.name,
		Content: c.content,
	}}, nil
}

// ── Format helpers ───────────────────────────────────────────────

// FormatSyncReport returns a human-readable summary of sync activity.
func FormatSyncReport(items []Item) string {
	if len(items) == 0 {
		return "No new content to sync."
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Synced %d items:\n", len(items)))
	for _, item := range items {
		b.WriteString(fmt.Sprintf("  - [%s] %s (%d chars)\n", item.Source, item.Path, len(item.Content)))
	}
	return b.String()
}
