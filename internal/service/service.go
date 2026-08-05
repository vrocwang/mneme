package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// HealthStatus represents the current health of a service.
type HealthStatus string

const (
	HealthUnknown  HealthStatus = "unknown"
	HealthStarting HealthStatus = "starting"
	HealthRunning  HealthStatus = "running"
	HealthDegraded HealthStatus = "degraded"
	HealthStopping HealthStatus = "stopping"
	HealthStopped  HealthStatus = "stopped"
	HealthFailed   HealthStatus = "failed"
)

// Service is a long-running component with lifecycle management.
type Service interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
}

// HealthChecker is an optional interface services can implement for
// health probe support.
type HealthChecker interface {
	Service
	Health(ctx context.Context) HealthStatus
}

// DependencyProvider is an optional interface for services that
// declare their dependencies. The Manager uses this for ordered
// startup and shutdown.
type DependencyProvider interface {
	Service
	Dependencies() []string
}

// ServiceInfo tracks runtime state for a registered service.
type ServiceInfo struct {
	Service   Service      `json:"-"`
	Name      string       `json:"name"`
	Status    HealthStatus `json:"status"`
	StartedAt *time.Time   `json:"started_at,omitempty"`
	StoppedAt *time.Time   `json:"stopped_at,omitempty"`
	Error     string       `json:"error,omitempty"`
}

// Manager orchestrates service lifecycle with health tracking and
// dependency-aware startup/shutdown ordering.
type Manager struct {
	mu       sync.RWMutex
	logger   *slog.Logger
	services map[string]*ServiceInfo
	order    []string // start order
}

// NewManager creates a service manager.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		logger:   logger.With("component", "service-manager"),
		services: make(map[string]*ServiceInfo),
	}
}

// Register adds a service to the manager.
func (m *Manager) Register(svc Service) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := svc.Name()
	if _, exists := m.services[name]; exists {
		m.logger.Warn("service already registered, replacing", "name", name)
	}
	m.services[name] = &ServiceInfo{
		Service: svc,
		Name:    name,
		Status:  HealthUnknown,
	}
	m.order = append(m.order, name)
}

// StartAll starts all registered services in dependency order.
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	ordered := m.resolveStartOrder()
	m.mu.Unlock()

	for _, name := range ordered {
		info := m.services[name]
		m.logger.Info("starting service", "name", name)

		info.Status = HealthStarting
		now := time.Now().UTC()
		info.StartedAt = &now

		if err := info.Service.Start(ctx); err != nil {
			info.Status = HealthFailed
			info.Error = err.Error()
			return fmt.Errorf("service %q: %w", name, err)
		}

		info.Status = HealthRunning
		info.Error = ""
	}
	return nil
}

// StopAll stops all registered services in reverse dependency order.
func (m *Manager) StopAll() {
	m.mu.RLock()
	ordered := m.resolveStartOrder()
	m.mu.RUnlock()

	// Reverse the order for shutdown.
	for i := len(ordered) - 1; i >= 0; i-- {
		name := ordered[i]
		info := m.services[name]
		m.logger.Info("stopping service", "name", name)

		info.Status = HealthStopping
		if err := info.Service.Stop(); err != nil {
			m.logger.Warn("error stopping service", "name", name, "error", err)
			info.Error = err.Error()
		}

		now := time.Now().UTC()
		info.StoppedAt = &now
		info.Status = HealthStopped
	}
}

// Health returns the health status for all services.
func (m *Manager) Health(ctx context.Context) map[string]HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]HealthStatus, len(m.services))
	for name, info := range m.services {
		if hc, ok := info.Service.(HealthChecker); ok {
			result[name] = hc.Health(ctx)
		} else {
			result[name] = info.Status
		}
	}
	return result
}

// Info returns detailed information about all registered services.
func (m *Manager) Info() []ServiceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ServiceInfo, 0, len(m.services))
	for _, name := range m.order {
		if info, ok := m.services[name]; ok {
			result = append(result, *info)
		}
	}
	return result
}

// Get returns a registered service by name.
func (m *Manager) Get(name string) Service {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if info, ok := m.services[name]; ok {
		return info.Service
	}
	return nil
}

func (m *Manager) resolveStartOrder() []string {
	// Build dependency graph.
	deps := make(map[string][]string)
	for _, name := range m.order {
		info := m.services[name]
		if dp, ok := info.Service.(DependencyProvider); ok {
			deps[name] = dp.Dependencies()
		}
	}

	// Topological sort.
	visited := make(map[string]bool)
	var order []string

	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		for _, dep := range deps[name] {
			if _, exists := m.services[dep]; exists {
				visit(dep)
			}
		}
		order = append(order, name)
	}

	sorted := make([]string, len(m.order))
	copy(sorted, m.order)
	sort.Strings(sorted) // deterministic starting point

	for _, name := range sorted {
		visit(name)
	}
	return order
}
