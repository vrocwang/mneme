package config

import (
	"fmt"
)

// WorkloadKind identifies the type of work a provider is being used for.
type WorkloadKind string

const (
	WorkloadChat       WorkloadKind = "chat"
	WorkloadCoding     WorkloadKind = "coding"
	WorkloadReasoning  WorkloadKind = "reasoning"
	WorkloadSummary    WorkloadKind = "summary"
	WorkloadMemory     WorkloadKind = "memory"
	WorkloadEmbeddings WorkloadKind = "embeddings"
	WorkloadVision     WorkloadKind = "vision"
)

// ProviderRouter selects a provider for a given workload.
type ProviderRouter struct {
	cfg *Config
}

// NewProviderRouter creates a router from config.
func NewProviderRouter(cfg *Config) *ProviderRouter {
	return &ProviderRouter{cfg: cfg}
}

// ProviderFor returns the provider config for a given workload.
// Falls back to the agent default model if no workload-specific override is set.
func (r *ProviderRouter) ProviderFor(kind WorkloadKind) (*ProviderConfig, error) {
	// Check per-workload route overrides from config.
	model := r.cfg.Agent.DefaultModel
	if route, ok := r.cfg.Agent.ModelRoutes[string(kind)]; ok && route != "" {
		model = route
	}
	p := r.cfg.FindProviderForModel(model)
	if p == nil {
		return nil, fmt.Errorf("no provider found for workload %q (model %q)", kind, model)
	}
	return p, nil
}

// ModelFor returns the model name for a given workload, resolving any overrides.
func (r *ProviderRouter) ModelFor(kind WorkloadKind) string {
	if route, ok := r.cfg.Agent.ModelRoutes[string(kind)]; ok && route != "" {
		return route
	}
	return r.cfg.Agent.DefaultModel
}
