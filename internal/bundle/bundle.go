// Package bundle defines the capability-bundle abstraction — the
// no-privileged-core composition unit. Each bundle is a named group of
// capabilities (tools, agents, or event-driven behaviors) that can be enabled
// or disabled via config. This mirrors deepseek-harness's "everything is a
// plugin" model, where the built-in agent set and core tools are just the
// first, always-present layer rather than a privileged core.
//
// The boot package assembles a Deps and drives the bundle registry; bundles
// live here so they can import leaf packages (capability, tools, agent, cron,
// subconscious, etc.) without creating an import cycle with boot.
package bundle

import (
	"context"
	"log/slog"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/pkg/dispose"
)

// BuiltinSetID is the capability-set ID under which all built-in (first-layer)
// bundles register their tools and agents. It must match the literal used by
// the rest of the boot path when registering user-defined agents.
const BuiltinSetID = "builtin"

// Deps carries the dependencies a bundle may consume. It is assembled by the
// boot layer and passed to each bundle's Register. Fields are added as new
// bundles require them; a bundle must tolerate a nil dependency it does not
// strictly need.
type Deps struct {
	Reg          *capability.CapabilityRegistry
	Cfg          *config.Config
	Workspace    string
	SecurityTier string
	Log          *slog.Logger

	// Search / network configuration for tool bundles.
	BraveAPIKey  string
	TavilyAPIKey string
	SearxngURL   string
	ProxyConfig  config.ProxyConfig
}

// Bundle is a named, independently enableable capability group. Register runs
// the bundle's side effects and returns an optional dispose func for unwinding
// on shutdown. A bundle that only contributes tools/agents registers them into
// the shared builtin set and returns a nil dispose (in-process builtins own no
// subprocesses).
type Bundle interface {
	ID() string
	Register(ctx context.Context, d *Deps) (dispose.Func, error)
}

// bundleFunc adapts a function to Bundle.
type bundleFunc struct {
	id string
	fn func(ctx context.Context, d *Deps) (dispose.Func, error)
}

func (b bundleFunc) ID() string { return b.id }
func (b bundleFunc) Register(ctx context.Context, d *Deps) (dispose.Func, error) {
	return b.fn(ctx, d)
}

// Func builds a Bundle from a name and a register function.
func Func(id string, fn func(ctx context.Context, d *Deps) (dispose.Func, error)) Bundle {
	return bundleFunc{id: id, fn: fn}
}

// Registry determines which bundles are enabled and runs them in order.
type Registry struct {
	disabled map[string]bool
}

// NewRegistry builds a registry from the config's disabled-bundle list.
func NewRegistry(disabled []string) *Registry {
	m := make(map[string]bool, len(disabled))
	for _, id := range disabled {
		m[id] = true
	}
	return &Registry{disabled: m}
}

// IsEnabled reports whether the bundle with the given ID should run.
func (r *Registry) IsEnabled(id string) bool {
	if r == nil {
		return true
	}
	return !r.disabled[id]
}

// Run executes each enabled bundle in order, collecting dispose funcs into a
// single composed dispose (LIFO). It stops at the first bundle that errors.
func (r *Registry) Run(ctx context.Context, d *Deps, bundles []Bundle) (dispose.Func, error) {
	var disposes []dispose.Func
	for _, b := range bundles {
		if !r.IsEnabled(b.ID()) {
			if d != nil && d.Log != nil {
				d.Log.Info("bundle disabled by config", "id", b.ID())
			}
			continue
		}
		disposeFn, err := b.Register(ctx, d)
		if err != nil {
			return nil, err
		}
		if disposeFn != nil {
			disposes = append(disposes, disposeFn)
		}
	}
	return dispose.Compose(disposes...), nil
}
