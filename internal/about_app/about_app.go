// Package about_app provides a dynamic capability directory that enumerates
// all registered RPC methods, agents, tools, channels, and schemas.
package about_app

import (
	"sort"
	"sync"
)

// Capability represents a single exposed feature (RPC method, agent, tool, channel).
type Capability struct {
	Kind        string `json:"kind"`        // "rpc", "agent", "tool", "channel", "skill"
	Name        string `json:"name"`        // unique identifier
	Description string `json:"description"` // human-readable summary
	Version     string `json:"version,omitempty"`
	Enabled     bool   `json:"enabled"`
	Hidden      bool   `json:"hidden,omitempty"`
	Source      string `json:"source,omitempty"` // "builtin", "extension", "custom"
}

// Directory tracks all registered capabilities.
type Directory struct {
	mu    sync.RWMutex
	items map[string]*Capability // keyed by "kind:name"
}

// NewDirectory creates a new capability directory.
func NewDirectory() *Directory {
	return &Directory{items: make(map[string]*Capability)}
}

// Register adds or updates a capability entry.
func (d *Directory) Register(c Capability) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := c.Kind + ":" + c.Name
	d.items[key] = &c
}

// Unregister removes a capability by kind and name.
func (d *Directory) Unregister(kind, name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.items, kind+":"+name)
}

// List returns all registered capabilities.
func (d *Directory) List() []Capability {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]Capability, 0, len(d.items))
	for _, c := range d.items {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Name < out[j].Name
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// ListByKind returns capabilities of a specific kind.
func (d *Directory) ListByKind(kind string) []Capability {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []Capability
	for _, c := range d.items {
		if c.Kind == kind {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Count returns the total number of registered capabilities.
func (d *Directory) Count() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.items)
}

// CountByKind returns counts grouped by kind.
func (d *Directory) CountByKind() map[string]int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	counts := make(map[string]int)
	for _, c := range d.items {
		counts[c.Kind]++
	}
	return counts
}

// Snapshot returns a full snapshot of the capability directory.
func (d *Directory) Snapshot() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	snapshot := map[string]interface{}{
		"total_count":  len(d.items),
		"by_kind":      d.countByKindLocked(),
		"capabilities": d.listLocked(),
	}
	return snapshot
}

// Internal helpers (caller must hold lock).
func (d *Directory) countByKindLocked() map[string]int {
	counts := make(map[string]int)
	for _, c := range d.items {
		counts[c.Kind]++
	}
	return counts
}

func (d *Directory) listLocked() []Capability {
	out := make([]Capability, 0, len(d.items))
	for _, c := range d.items {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Name < out[j].Name
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
