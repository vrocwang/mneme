// Package sources provides external data source connectors for the learning
// engine. Sources enrich the user context with data from LinkedIn, GitHub,
// and other external platforms. Each connector is registered and loaded on
// demand — no source is hardcoded into the core learning engine.
package sources

import "context"

// Connector fetches user context data from an external source.
type Connector interface {
	// Name returns a unique identifier for this source (e.g. "linkedin", "github").
	Name() string
	// Fetch retrieves context data. Returns key-value pairs suitable for the
	// learning engine's preference extraction.
	Fetch(ctx context.Context, config map[string]string) ([]ContextPair, error)
	// RequiresAuth returns true if this source requires OAuth or API key auth.
	RequiresAuth() bool
}

// ContextPair is a key-value context item extracted from an external source.
type ContextPair struct {
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source"`
}

// Registry holds registered external source connectors.
type Registry struct {
	connectors map[string]Connector
}

// NewRegistry creates a connector registry.
func NewRegistry() *Registry {
	return &Registry{connectors: make(map[string]Connector)}
}

// Register adds a connector to the registry.
func (r *Registry) Register(c Connector) {
	r.connectors[c.Name()] = c
}

// List returns the names of all registered connectors.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.connectors))
	for name := range r.connectors {
		names = append(names, name)
	}
	return names
}

// FetchAll runs all registered connectors and aggregates results.
func (r *Registry) FetchAll(ctx context.Context, configs map[string]map[string]string) ([]ContextPair, error) {
	var all []ContextPair
	for name, conn := range r.connectors {
		cfg := configs[name]
		pairs, err := conn.Fetch(ctx, cfg)
		if err != nil {
			continue // skip failed connectors
		}
		all = append(all, pairs...)
	}
	return all, nil
}
