// Package routing provides model/provider routing with quality, reliability,
// and cost-based selection. Supports failover between providers and per-task
// model selection.
package routing

import (
	"strings"
	"sync"
)

// RouteKind selects the routing strategy.
type RouteKind string

const (
	RouteDefault   RouteKind = "default"
	RouteCoding    RouteKind = "coding"
	RouteReasoning RouteKind = "reasoning"
	RouteSummary   RouteKind = "summary"
	RouteVision    RouteKind = "vision"
)

// ModelRoute maps a task kind to a preferred model.
type ModelRoute struct {
	Kind  RouteKind `json:"kind"`
	Model string    `json:"model"`
}

// Router manages model selection and provider failover.
type Router struct {
	mu       sync.RWMutex
	routes   map[RouteKind]string // task kind -> model
	fallback string               // default fallback model
}

// NewRouter creates a model router.
func NewRouter(fallbackModel string) *Router {
	return &Router{
		routes:   make(map[RouteKind]string),
		fallback: fallbackModel,
	}
}

// SetRoute configures a model for a task kind.
func (r *Router) SetRoute(kind RouteKind, model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes[kind] = model
}

// ProviderModel describes a provider's available models for routing configuration.
type ProviderModel struct {
	Name   string
	Models []string
}

// ConfigureFromProviders sets routes based on available provider models.
// - Reasoning/strong models → coding + reasoning tasks
// - Cheap/fast models → summary tasks
// - Vision-capable models → vision tasks
// Routes that are already set to a non-default value are left alone.
func (r *Router) ConfigureFromProviders(providers []ProviderModel) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, p := range providers {
		for _, m := range p.Models {
			lower := strings.ToLower(m)
			switch {
			case strings.Contains(lower, "reasoning") ||
				strings.Contains(lower, "opus") ||
				strings.Contains(lower, "thinking") ||
				strings.Contains(lower, "pro"):
				if r.routes[RouteReasoning] == "" || r.routes[RouteReasoning] == r.fallback {
					r.routes[RouteReasoning] = m
				}
				if r.routes[RouteCoding] == "" || r.routes[RouteCoding] == r.fallback {
					r.routes[RouteCoding] = m
				}
			case strings.Contains(lower, "haiku") ||
				strings.Contains(lower, "flash") ||
				strings.Contains(lower, "mini") ||
				strings.Contains(lower, "nano"):
				if r.routes[RouteSummary] == "" || r.routes[RouteSummary] == r.fallback {
					r.routes[RouteSummary] = m
				}
			case strings.Contains(lower, "vision") ||
				strings.Contains(lower, "sonnet"):
				if r.routes[RouteVision] == "" || r.routes[RouteVision] == r.fallback {
					r.routes[RouteVision] = m
				}
			}
		}
	}
	// Set RouteDefault to the best available actual model so companions
	// and other non-specialized consumers get a real API model name.
	if r.routes[RouteDefault] == "" || r.routes[RouteDefault] == r.fallback {
		if m := r.routes[RouteReasoning]; m != "" {
			r.routes[RouteDefault] = m
		} else if m := r.routes[RouteCoding]; m != "" {
			r.routes[RouteDefault] = m
		} else if len(providers) > 0 && len(providers[0].Models) > 0 {
			r.routes[RouteDefault] = providers[0].Models[0]
		}
	}
}

// Resolve returns the best model for a task kind, falling back to default.
func (r *Router) Resolve(kind RouteKind) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if model, ok := r.routes[kind]; ok && model != "" {
		return model
	}
	return r.fallback
}

// DefaultRoutes returns sensible defaults for common task kinds.
func DefaultRoutes() []ModelRoute {
	return []ModelRoute{
		{Kind: RouteCoding, Model: ""}, // empty = use fallback
		{Kind: RouteReasoning, Model: ""},
		{Kind: RouteSummary, Model: ""},
		{Kind: RouteVision, Model: ""},
	}
}

// ClassifyTaskKind maps a user message to a RouteKind for model selection.
func ClassifyTaskKind(msg string) RouteKind {
	lower := strings.ToLower(msg)
	for _, w := range []string{"code", "fix", "bug", "implement", "refactor", "function", "class", "test", "build", "compile", "error:", "panic:", "crash"} {
		if strings.Contains(lower, w) {
			return RouteCoding
		}
	}
	for _, w := range []string{"why", "analyze", "explain", "compare", "evaluate", "reason", "think", "philosophy", "strategy"} {
		if strings.Contains(lower, w) {
			return RouteReasoning
		}
	}
	for _, w := range []string{"summarize", "summary", "tldr", "condense", "recap", "brief"} {
		if strings.Contains(lower, w) {
			return RouteSummary
		}
	}
	return RouteDefault
}
