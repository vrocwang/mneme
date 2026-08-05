// Package app_state tracks application lifecycle state: connection status,
// startup phases, subsystem health, and global readiness.
package app_state

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Phase represents a startup or lifecycle phase.
type Phase string

const (
	PhaseBooting      Phase = "booting"
	PhaseLoading      Phase = "loading"
	PhaseConnecting   Phase = "connecting"
	PhaseReady        Phase = "ready"
	PhaseDegraded     Phase = "degraded"
	PhaseShuttingDown Phase = "shutting_down"
	PhaseError        Phase = "error"
)

// Status holds the current application state.
type Status struct {
	Phase         Phase             `json:"phase"`
	StartedAt     time.Time         `json:"started_at"`
	ReadyAt       time.Time         `json:"ready_at,omitempty"`
	UptimeSeconds float64           `json:"uptime_seconds"`
	Subsystems    map[string]Health `json:"subsystems"`
	Version       string            `json:"version"`
}

// Health describes a subsystem's health.
type Health struct {
	Status  string    `json:"status"` // "healthy", "degraded", "down", "starting"
	Message string    `json:"message,omitempty"`
	Since   time.Time `json:"since"`
}

// Manager tracks application state and subsystem health.
type Manager struct {
	mu         sync.RWMutex
	phase      Phase
	startedAt  time.Time
	readyAt    time.Time
	version    string
	subsystems map[string]*Health
	watchers   []func(Phase)
	log        *slog.Logger
}

// NewManager creates a new application state manager.
func NewManager(version string, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		phase:      PhaseBooting,
		startedAt:  time.Now(),
		version:    version,
		subsystems: make(map[string]*Health),
		log:        log.With("component", "app_state"),
	}
}

// Transition changes the application phase.
func (m *Manager) Transition(phase Phase) {
	m.mu.Lock()
	prev := m.phase
	m.phase = phase
	if phase == PhaseReady && m.readyAt.IsZero() {
		m.readyAt = time.Now()
	}
	watchers := append([]func(Phase){}, m.watchers...)
	m.mu.Unlock()

	m.log.Info("phase transition", "from", prev, "to", phase)
	for _, w := range watchers {
		w(phase)
	}
}

// Current returns the current application status.
func (m *Manager) Current() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	subs := make(map[string]Health, len(m.subsystems))
	for name, h := range m.subsystems {
		subs[name] = *h
	}

	return Status{
		Phase:         m.phase,
		StartedAt:     m.startedAt,
		ReadyAt:       m.readyAt,
		UptimeSeconds: time.Since(m.startedAt).Seconds(),
		Subsystems:    subs,
		Version:       m.version,
	}
}

// Phase returns the current phase.
func (m *Manager) Phase() Phase {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.phase
}

// IsReady returns true if the application is ready to serve.
func (m *Manager) IsReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.phase == PhaseReady || m.phase == PhaseDegraded
}

// SetSubsystemHealth updates the health status of a named subsystem.
func (m *Manager) SetSubsystemHealth(name, status, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.subsystems[name]; ok {
		existing.Status = status
		existing.Message = message
		existing.Since = time.Now()
	} else {
		m.subsystems[name] = &Health{Status: status, Message: message, Since: time.Now()}
	}
	m.log.Debug("subsystem health", "name", name, "status", status)
}

// SubsystemHealth returns a subsystem's health.
func (m *Manager) SubsystemHealth(name string) (Health, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h, ok := m.subsystems[name]
	if !ok {
		return Health{}, false
	}
	return *h, true
}

// OnPhaseChange registers a callback for phase transitions.
// Returns a function to unregister the callback.
func (m *Manager) OnPhaseChange(fn func(Phase)) func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchers = append(m.watchers, fn)
	idx := len(m.watchers) - 1
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if idx < len(m.watchers) {
			m.watchers = append(m.watchers[:idx], m.watchers[idx+1:]...)
		}
	}
}

// WaitUntilReady blocks until the application is ready or the context is cancelled.
func (m *Manager) WaitUntilReady(ctx context.Context) error {
	if m.IsReady() {
		return nil
	}

	ready := make(chan struct{})
	remove := m.OnPhaseChange(func(p Phase) {
		if p == PhaseReady || p == PhaseDegraded {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	defer remove()

	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
