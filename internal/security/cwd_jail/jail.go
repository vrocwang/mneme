// Package cwd_jail provides a cross-platform directory jail abstraction
// for agent tool execution. Each Jail describes a sandbox configuration
// (root directory, read-only flag, network/subprocess permissions), and
// the platform-specific backend enforces it.
//
// This sits above the low-level sandbox package and provides a declarative,
// named-jail model matching the Rust cwd_jail domain.
package cwd_jail

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Jail describes a sandbox configuration for tool execution.
type Jail struct {
	// Root is the directory the jailed process sees as its working directory.
	// Must be an absolute, canonical path before passing to Spawn.
	Root string

	// ReadOnly prevents write access within the jail root.
	ReadOnly bool

	// AllowNet permits network access from within the jail.
	AllowNet bool

	// AllowSubprocess permits spawning child processes from within the jail.
	AllowSubprocess bool

	// Label is a human-readable name for diagnostics and registry tracking.
	Label string
}

// Backend enforces jail restrictions on command execution.
// Each platform provides its own implementation.
type Backend interface {
	// Name returns a human-readable backend name.
	Name() string

	// IsAvailable reports whether this backend can enforce restrictions.
	IsAvailable() bool

	// Spawn prepares a command to run inside the given jail.
	// The returned Cmd is ready to start; the caller calls cmd.Run() or
	// cmd.Start() + cmd.Wait().
	Spawn(ctx context.Context, jail *Jail, cmd string, args ...string) (*exec.Cmd, error)
}

// Registry tracks active jails for diagnostics and cleanup.
type Registry struct {
	mu     sync.Mutex
	jails  map[string]*Jail
	active map[string]bool
}

// NewRegistry creates a new jail registry.
func NewRegistry() *Registry {
	return &Registry{
		jails:  make(map[string]*Jail),
		active: make(map[string]bool),
	}
}

// Register adds a jail to the registry.
func (r *Registry) Register(jail *Jail) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jails[jail.Label] = jail
}

// Unregister removes a jail from the registry.
func (r *Registry) Unregister(label string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.jails, label)
	delete(r.active, label)
}

// MarkActive marks a jail as currently executing a command.
func (r *Registry) MarkActive(label string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active[label] = true
}

// MarkInactive marks a jail as no longer executing.
func (r *Registry) MarkInactive(label string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active[label] = false
}

// List returns all registered jails.
func (r *Registry) List() []*Jail {
	r.mu.Lock()
	defer r.mu.Unlock()
	jails := make([]*Jail, 0, len(r.jails))
	for _, j := range r.jails {
		jails = append(jails, j)
	}
	return jails
}

// ActiveCount returns the number of active jails.
func (r *Registry) ActiveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, active := range r.active {
		if active {
			count++
		}
	}
	return count
}

// ── Top-level API ─────────────────────────────────────────────────────

var (
	globalBackend Backend
	globalReg     = NewRegistry()
	backendOnce   sync.Once
)

// Spawn prepares a command to run inside the given jail using the
// platform-detected backend. The jail root is canonicalized before
// being passed to the backend.
func Spawn(ctx context.Context, jail *Jail, cmd string, args ...string) (*exec.Cmd, error) {
	backendOnce.Do(func() {
		globalBackend = Detect()
	})

	if globalBackend == nil {
		return nil, fmt.Errorf("cwd_jail: no backend available")
	}

	// Canonicalize the jail root to prevent .. / symlink bypass.
	abs, err := filepath.Abs(jail.Root)
	if err != nil {
		return nil, fmt.Errorf("cwd_jail: resolve root %q: %w", jail.Root, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// If the path doesn't exist yet, use the absolute path.
		resolved = filepath.Clean(abs)
	}
	jail.Root = resolved

	// Register for tracking.
	globalReg.Register(jail)
	globalReg.MarkActive(jail.Label)

	return globalBackend.Spawn(ctx, jail, cmd, args...)
}

// BackendName returns the name of the active backend.
func BackendName() string {
	backendOnce.Do(func() {
		globalBackend = Detect()
	})
	if globalBackend == nil {
		return "none"
	}
	return globalBackend.Name()
}

// GetRegistry returns the global jail registry.
func GetRegistry() *Registry {
	return globalReg
}

// IsPathJailed returns true if the given path is within any registered
// jail root.
func IsPathJailed(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	clean := filepath.Clean(abs)

	for _, jail := range globalReg.List() {
		jailRoot := filepath.Clean(jail.Root)
		if clean == jailRoot {
			return true
		}
		if len(clean) > len(jailRoot) && clean[len(jailRoot)] == os.PathSeparator &&
			clean[:len(jailRoot)] == jailRoot {
			return true
		}
	}
	return false
}
