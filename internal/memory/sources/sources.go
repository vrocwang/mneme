// Package sources provides a registry of user-configured data connectors
// that feed external content into the memory pipeline.
package sources

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind categorizes a data source.
type Kind string

const (
	KindFolder   Kind = "folder"
	KindGitHub   Kind = "github_repo"
	KindRSS      Kind = "rss_feed"
	KindWebPage  Kind = "web_page"
	KindComposio Kind = "composio"
	KindMCP      Kind = "mcp_server"
	KindManual   Kind = "manual"
)

// Source is a user-configured data connector.
type Source struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Kind       Kind              `json:"kind"`
	Target     string            `json:"target"` // path, URL, repo name, etc.
	Enabled    bool              `json:"enabled"`
	AutoSync   bool              `json:"auto_sync"`  // sync automatically
	SyncEvery  string            `json:"sync_every"` // "hourly", "daily", "6h", "30m", ""
	LastSyncAt time.Time         `json:"last_sync_at,omitempty"`
	LastStatus string            `json:"last_status"` // "ok", "error", "never"
	ItemCount  int               `json:"item_count"`
	Config     map[string]string `json:"config,omitempty"` // kind-specific settings
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// Summary returns a brief human-readable description of the source.
func (s *Source) Summary() string {
	status := s.LastStatus
	if status == "" {
		status = "never"
	}
	enabled := ""
	if !s.Enabled {
		enabled = " [disabled]"
	}
	return fmt.Sprintf("[%s] %s → %s (%d items, %s)%s",
		s.Kind, s.Name, s.Target, s.ItemCount, status, enabled)
}

// Registry manages user-configured data sources.
type Registry struct {
	mu      sync.RWMutex
	sources map[string]*Source
}

// NewRegistry creates a source registry.
func NewRegistry() *Registry {
	return &Registry{sources: make(map[string]*Source)}
}

// Register adds or updates a source configuration.
func (r *Registry) Register(s *Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	r.sources[s.ID] = s
}

// Get retrieves a source by ID.
func (r *Registry) Get(id string) *Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sources[id]
}

// Remove deletes a source.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sources, id)
}

// List returns all sources sorted by name.
func (r *Registry) List() []*Source {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Source, 0, len(r.sources))
	for _, s := range r.sources {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// ListByKind returns sources of a specific kind.
func (r *Registry) ListByKind(kind Kind) []*Source {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*Source
	for _, s := range r.sources {
		if s.Kind == kind {
			result = append(result, s)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// ListEnabled returns all enabled sources that are due for sync.
func (r *Registry) ListEnabled() []*Source {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*Source
	for _, s := range r.sources {
		if s.Enabled {
			result = append(result, s)
		}
	}
	return result
}

// ListDueForSync returns enabled sources whose sync interval has elapsed.
func (r *Registry) ListDueForSync() []*Source {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*Source
	now := time.Now()
	for _, s := range r.sources {
		if !s.Enabled {
			continue
		}
		if s.AutoSync && isDue(s.LastSyncAt, s.SyncEvery, now) {
			result = append(result, s)
		}
	}
	return result
}

// MarkSynced updates LastSyncAt and LastStatus for a source.
func (r *Registry) MarkSynced(id, status string, itemCount int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sources[id]; ok {
		s.LastSyncAt = time.Now()
		s.LastStatus = status
		s.ItemCount += itemCount
	}
}

// Stats returns summary statistics.
func (r *Registry) Stats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var total, enabled, errors int
	byKind := make(map[Kind]int)
	for _, s := range r.sources {
		total++
		if s.Enabled {
			enabled++
		}
		if s.LastStatus == "error" {
			errors++
		}
		byKind[s.Kind]++
	}
	return map[string]interface{}{
		"total":   total,
		"enabled": enabled,
		"errors":  errors,
		"by_kind": byKind,
	}
}

// FormatList returns a human-readable listing of sources.
func FormatList(sources []*Source) string {
	if len(sources) == 0 {
		return "No data sources configured."
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Data sources (%d):\n", len(sources)))
	for _, s := range sources {
		b.WriteString("  " + s.Summary() + "\n")
	}
	return b.String()
}

// ── Helpers ─────────────────────────────────────────────────────

func isDue(lastSync time.Time, interval string, now time.Time) bool {
	if lastSync.IsZero() {
		return true // never synced
	}
	d := parseInterval(interval)
	if d == 0 {
		return false
	}
	return now.Sub(lastSync) >= d
}

func parseInterval(s string) time.Duration {
	switch s {
	case "hourly":
		return time.Hour
	case "daily":
		return 24 * time.Hour
	case "weekly":
		return 7 * 24 * time.Hour
	case "":
		return 0
	}
	// Parse Go duration format: "6h", "30m", "5m"
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}
