// Package dashboard provides aggregated application statistics and health
// metrics for the frontend dashboard view.
package dashboard

import (
	"sync"
	"time"
)

// Metrics holds aggregated application statistics.
type Metrics struct {
	TotalThreads    int           `json:"total_threads"`
	TotalMessages   int           `json:"total_messages"`
	TotalToolCalls  int           `json:"total_tool_calls"`
	MemoryChunks    int           `json:"memory_chunks"`
	ActiveSessions  int           `json:"active_sessions"`
	Uptime          time.Duration `json:"uptime_seconds"`
	CostTotalUSD    float64       `json:"cost_total_usd"`
	AgentsAvailable int           `json:"agents_available"`
	ToolsAvailable  int           `json:"tools_available"`
	ChannelsActive  []string      `json:"channels_active"`
	Notifications   int           `json:"unread_notifications"`
}

// Snapshot is a point-in-time capture of application state for the dashboard.
type Snapshot struct {
	Metrics    Metrics   `json:"metrics"`
	CapturedAt time.Time `json:"captured_at"`
}

// Collector gathers metrics from live application state.
type Collector struct {
	mu      sync.RWMutex
	metrics Metrics
}

// NewCollector creates a dashboard metrics collector.
func NewCollector() *Collector {
	return &Collector{
		metrics: Metrics{ChannelsActive: []string{}},
	}
}

// Update atomically sets the current metrics.
func (c *Collector) Update(m Metrics) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics = m
}

// Snapshot returns a point-in-time copy of the current metrics.
func (c *Collector) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Snapshot{
		Metrics:    c.metrics,
		CapturedAt: time.Now().UTC(),
	}
}

// IncrementToolCalls atomically increments the tool call counter.
func (c *Collector) IncrementToolCalls() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics.TotalToolCalls++
}

// AddCost atomically adds to the total cost.
func (c *Collector) AddCost(usd float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics.CostTotalUSD += usd
}
