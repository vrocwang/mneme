// Package health provides health checking and metrics for the application.
// It offers a lightweight in-process metrics registry (no external dependencies)
// and aggregates health status from registered components.
package health

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

// ── Counter ──────────────────────────────────────────────────────────────

// Counter is a monotonically increasing counter.
type Counter struct {
	value atomic.Int64
}

func (c *Counter) Inc()                      { c.value.Add(1) }
func (c *Counter) Add(n int64)               { c.value.Add(n) }
func (c *Counter) Value() int64              { return c.value.Load() }
func (c *Counter) Snapshot() CounterSnapshot { return CounterSnapshot{Value: c.value.Load()} }

type CounterSnapshot struct {
	Value int64 `json:"value"`
}

// ── Gauge ────────────────────────────────────────────────────────────────

// Gauge is a value that can go up and down.
type Gauge struct {
	value atomic.Int64
}

func (g *Gauge) Set(v int64)             { g.value.Store(v) }
func (g *Gauge) Add(delta int64)         { g.value.Add(delta) }
func (g *Gauge) Value() int64            { return g.value.Load() }
func (g *Gauge) Snapshot() GaugeSnapshot { return GaugeSnapshot{Value: g.value.Load()} }

type GaugeSnapshot struct {
	Value int64 `json:"value"`
}

// ── Histogram ────────────────────────────────────────────────────────────

// Histogram tracks the distribution of values using exponential buckets.
type Histogram struct {
	mu      sync.Mutex
	buckets []int64 // bucket upper bounds (e.g., 1, 5, 25, 125, 625, ...)
	counts  []int64
	sum     int64
	count   int64
	min     int64
	max     int64
}

// NewHistogram creates a histogram with exponential buckets starting from start
// and multiplying by factor each step.
func NewHistogram(start int64, factor int64, numBuckets int) *Histogram {
	buckets := make([]int64, numBuckets)
	v := start
	for i := 0; i < numBuckets; i++ {
		buckets[i] = v
		v *= factor
	}
	return &Histogram{
		buckets: buckets,
		counts:  make([]int64, numBuckets+1), // +1 for overflow bucket
		min:     1<<63 - 1,                   // max int64
	}
}

// Observe records a value in the histogram.
func (h *Histogram) Observe(v int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.count++
	h.sum += v
	if v < h.min {
		h.min = v
	}
	if v > h.max {
		h.max = v
	}

	for i, bound := range h.buckets {
		if v <= bound {
			h.counts[i]++
			return
		}
	}
	h.counts[len(h.buckets)]++ // overflow
}

func (h *Histogram) Snapshot() HistogramSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	snap := HistogramSnapshot{
		Count:   h.count,
		Sum:     h.sum,
		Buckets: make([]BucketSnapshot, 0, len(h.buckets)+1),
	}
	if h.count > 0 {
		snap.Min = h.min
		snap.Max = h.max
		snap.Avg = float64(h.sum) / float64(h.count)
	}

	for i, bound := range h.buckets {
		if h.counts[i] > 0 || i > 0 {
			snap.Buckets = append(snap.Buckets, BucketSnapshot{
				Le:    bound,
				Count: h.counts[i],
			})
		}
	}
	// Overflow bucket
	if h.counts[len(h.buckets)] > 0 {
		snap.Buckets = append(snap.Buckets, BucketSnapshot{
			Le:    -1, // sentinel for +Inf
			Count: h.counts[len(h.buckets)],
		})
	}

	return snap
}

type HistogramSnapshot struct {
	Count   int64            `json:"count"`
	Sum     int64            `json:"sum"`
	Min     int64            `json:"min,omitempty"`
	Max     int64            `json:"max,omitempty"`
	Avg     float64          `json:"avg,omitempty"`
	Buckets []BucketSnapshot `json:"buckets,omitempty"`
}

type BucketSnapshot struct {
	Le    int64 `json:"le"`
	Count int64 `json:"count"`
}

// ── Timer ────────────────────────────────────────────────────────────────

// Timer measures durations and records them in an underlying histogram.
// The zero value is safe but no-op until configured.
type Timer struct {
	hist *Histogram
}

// NewTimer creates a timer backed by the given histogram.
func NewTimer(hist *Histogram) *Timer {
	return &Timer{hist: hist}
}

// Duration records a completed duration.
func (t *Timer) Duration(d time.Duration) {
	if t.hist != nil {
		t.hist.Observe(d.Milliseconds())
	}
}

// Since records the duration since the given start time.
func (t *Timer) Since(start time.Time) {
	if t.hist != nil {
		t.hist.Observe(time.Since(start).Milliseconds())
	}
}

// ── Registry ─────────────────────────────────────────────────────────────

// Registry is a thread-safe collection of named metrics.
type Registry struct {
	mu         sync.RWMutex
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
	labels     map[string]string // static key=value labels attached to all metrics
}

// NewRegistry creates a new metrics registry.
func NewRegistry() *Registry {
	return &Registry{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
		labels:     make(map[string]string),
	}
}

// WithLabel adds a static label to the registry.
func (r *Registry) WithLabel(k, v string) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.labels[k] = v
	return r
}

// Counter returns or creates a named counter.
func (r *Registry) Counter(name string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c
	}
	c := &Counter{}
	r.counters[name] = c
	return c
}

// Gauge returns or creates a named gauge.
func (r *Registry) Gauge(name string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gauges[name]; ok {
		return g
	}
	g := &Gauge{}
	r.gauges[name] = g
	return g
}

// Histogram returns or creates a named histogram.
func (r *Registry) Histogram(name string) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.histograms[name]; ok {
		return h
	}
	h := NewHistogram(5, 5, 8) // 5, 25, 125, 625, 3125, 15625, 78125, 390625 ms
	r.histograms[name] = h
	return h
}

// Snapshot returns a point-in-time snapshot of all metrics.
func (r *Registry) Snapshot() MetricsSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snap := MetricsSnapshot{
		Labels:     make(map[string]string),
		Counters:   make(map[string]CounterSnapshot),
		Gauges:     make(map[string]GaugeSnapshot),
		Histograms: make(map[string]HistogramSnapshot),
	}
	for k, v := range r.labels {
		snap.Labels[k] = v
	}
	for k, c := range r.counters {
		snap.Counters[k] = c.Snapshot()
	}
	for k, g := range r.gauges {
		snap.Gauges[k] = g.Snapshot()
	}
	for k, h := range r.histograms {
		snap.Histograms[k] = h.Snapshot()
	}
	return snap
}

// MetricsSnapshot is a JSON-serializable snapshot of all registry metrics.
type MetricsSnapshot struct {
	Labels     map[string]string            `json:"labels,omitempty"`
	Counters   map[string]CounterSnapshot   `json:"counters,omitempty"`
	Gauges     map[string]GaugeSnapshot     `json:"gauges,omitempty"`
	Histograms map[string]HistogramSnapshot `json:"histograms,omitempty"`
}

// ToJSON marshals the snapshot to JSON.
func (s MetricsSnapshot) ToJSON() ([]byte, error) {
	return json.Marshal(s)
}

// ── Convenience constructors ─────────────────────────────────────────────

// DefaultAgentMetrics creates the standard set of agent-related metrics.
func DefaultAgentMetrics(r *Registry) (turns *Counter, turnDuration *Histogram, errors *Counter) {
	return r.Counter("agent_turns_total"),
		r.Histogram("agent_turn_duration_ms"),
		r.Counter("agent_errors_total")
}

// DefaultToolMetrics creates the standard set of tool-related metrics.
func DefaultToolMetrics(r *Registry) (calls *Counter, callDuration *Histogram, failures *Counter) {
	return r.Counter("tool_calls_total"),
		r.Histogram("tool_call_duration_ms"),
		r.Counter("tool_failures_total")
}

// DefaultMemoryMetrics creates the standard set of memory-related metrics.
func DefaultMemoryMetrics(r *Registry) (ingested *Counter, searches *Counter, searchDuration *Histogram) {
	return r.Counter("memory_ingested_total"),
		r.Counter("memory_searches_total"),
		r.Histogram("memory_search_duration_ms")
}
