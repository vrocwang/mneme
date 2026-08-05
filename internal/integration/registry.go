package integration

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

// registry is the concrete implementation of IntegrationRegistry.
type registry struct {
	mu         sync.RWMutex
	providers  map[string]OAuthProvider
	connectors map[string]SyncConnector
	log        *slog.Logger
}

// NewRegistry creates a new integration registry.
func NewRegistry(log *slog.Logger) IntegrationRegistry {
	if log == nil {
		log = slog.Default()
	}
	return &registry{
		providers:  make(map[string]OAuthProvider),
		connectors: make(map[string]SyncConnector),
		log:        log.With("component", "integration-registry"),
	}
}

// ── OAuth providers ──────────────────────────────────────────────────────

func (r *registry) RegisterOAuthProvider(p OAuthProvider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[p.ID()]; exists {
		r.log.Warn("oauth provider overwritten", "id", p.ID())
	}
	r.providers[p.ID()] = p
	r.log.Info("oauth provider registered", "id", p.ID(), "name", p.Name())
	return nil
}

func (r *registry) UnregisterOAuthProvider(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[id]; !ok {
		return fmt.Errorf("oauth provider %q not found", id)
	}
	delete(r.providers, id)
	r.log.Info("oauth provider unregistered", "id", id)
	return nil
}

func (r *registry) ListProviders() []ProviderDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderDescriptor, 0, len(r.providers))
	for _, p := range r.providers {
		authURL, _ := p.AuthURL("")
		out = append(out, ProviderDescriptor{
			ID:      p.ID(),
			Name:    p.Name(),
			AuthURL: authURL,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *registry) GetProvider(id string) (OAuthProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	if !ok {
		return nil, fmt.Errorf("oauth provider %q not found", id)
	}
	return p, nil
}

// ── Sync connectors ───────────────────────────────────────────────────────

func (r *registry) RegisterSyncConnector(c SyncConnector) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.connectors[c.ID()]; exists {
		r.log.Warn("sync connector overwritten", "id", c.ID())
	}
	r.connectors[c.ID()] = c
	r.log.Info("sync connector registered", "id", c.ID(), "kind", c.Kind())
	return nil
}

func (r *registry) UnregisterSyncConnector(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.connectors[id]; !ok {
		return fmt.Errorf("sync connector %q not found", id)
	}
	delete(r.connectors, id)
	r.log.Info("sync connector unregistered", "id", id)
	return nil
}

func (r *registry) ListConnectors() []ConnectorDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ConnectorDescriptor, 0, len(r.connectors))
	for _, c := range r.connectors {
		status := c.Status()
		out = append(out, ConnectorDescriptor{
			ID:        c.ID(),
			Kind:      c.Kind(),
			Name:      c.Name(),
			Connected: status.Connected,
			Status:    status,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *registry) GetConnector(id string) (SyncConnector, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.connectors[id]
	if !ok {
		return nil, fmt.Errorf("sync connector %q not found", id)
	}
	return c, nil
}
