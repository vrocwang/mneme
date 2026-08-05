// Package observability provides error reporting (Sentry) and lightweight
// tracing hooks for the Mneme desktop app. OpenTelemetry integration is
// deferred — the Tracer interface is designed to accept an OTel backend later.
package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

// ── Tracer abstraction (thread-safe, OTel-compatible shape) ─────────────────

// Span represents a unit of work with a name and attributes.
type Span struct {
	Name       string
	StartTime  time.Time
	Attributes map[string]string
}

// Tracer is a lightweight tracing interface. A no-op implementation is used
// when no backend is configured. Replace with an OTel tracer provider when ready.
type Tracer interface {
	// StartSpan begins a new span. The returned function finishes it.
	StartSpan(ctx context.Context, name string, attrs ...string) (context.Context, func())
}

// ── Error reporter ──────────────────────────────────────────────────────────

// ErrorReporter captures errors and sends them to a backend (Sentry or no-op).
type ErrorReporter interface {
	CaptureException(err error, tags map[string]string)
	CaptureMessage(msg string, level string, tags map[string]string)
	Flush(timeout time.Duration) bool
	Close()
}

// ── Hub ─────────────────────────────────────────────────────────────────────

// Hub is the central observability coordinator. It holds a tracer and an error
// reporter, and provides convenience methods for common instrumentations.
type Hub struct {
	tracer     Tracer
	reporter   ErrorReporter
	log        *slog.Logger
	fileWriter io.WriteCloser // app.jsonl log file (closed on shutdown)
	mu         sync.RWMutex
}

// NewHub creates an observability hub. If reporter or tracer are nil, no-op
// implementations are used so calling code never needs nil checks.
func NewHub(reporter ErrorReporter, tracer Tracer, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	if reporter == nil {
		reporter = &noopReporter{}
	}
	if tracer == nil {
		tracer = &noopTracer{}
	}
	return &Hub{
		tracer:   tracer,
		reporter: reporter,
		log:      log.With("component", "observability"),
	}
}

// Logger returns the hub's configured logger.
func (h *Hub) Logger() *slog.Logger { return h.log }

// SetReporter replaces the error reporter at runtime.
func (h *Hub) SetReporter(r ErrorReporter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r == nil {
		r = &noopReporter{}
	}
	h.reporter = r
}

// SetTracer replaces the tracer at runtime.
func (h *Hub) SetTracer(t Tracer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t == nil {
		t = &noopTracer{}
	}
	h.tracer = t
}

// ── Panic recovery ──────────────────────────────────────────────────────────

// RecoverPanic should be called via defer in goroutines and RPC handlers.
// It captures the panic stack and reports it, then re-panics if told to.
//
// Usage:
//
//	defer observability.Hub.RecoverPanic(ctx, true)
func (h *Hub) RecoverPanic(ctx context.Context, repanic bool) {
	if r := recover(); r != nil {
		stack := string(debug.Stack())
		err := fmt.Errorf("panic: %v\n%s", r, stack)
		h.log.Error("panic recovered", "error", err)

		h.mu.RLock()
		reporter := h.reporter
		h.mu.RUnlock()

		reporter.CaptureException(err, map[string]string{
			"mechanism": "panic",
			"context":   fmt.Sprint(ctx),
		})
		reporter.Flush(2 * time.Second)

		if repanic {
			panic(r)
		}
	}
}

// ── RPC middleware ──────────────────────────────────────────────────────────

// RPCMiddleware wraps an RPC handler with tracing and panic recovery.
// Usage:
//
//	hub.RPCMiddleware(ctx, "agent.chat", func(ctx context.Context) { ... })
func (h *Hub) RPCMiddleware(ctx context.Context, method string, fn func(ctx context.Context) error) error {
	h.mu.RLock()
	tracer := h.tracer
	h.mu.RUnlock()

	ctx, finish := tracer.StartSpan(ctx, "rpc."+method, "method", method)
	defer finish()
	defer h.RecoverPanic(ctx, false)

	start := time.Now()
	err := fn(ctx)

	duration := time.Since(start)
	if err != nil {
		h.log.Warn("rpc error", "method", method, "error", err, "duration_ms", duration.Milliseconds())
		h.mu.RLock()
		reporter := h.reporter
		h.mu.RUnlock()
		reporter.CaptureException(err, map[string]string{
			"method":      method,
			"duration_ms": fmt.Sprint(duration.Milliseconds()),
		})
	}

	return err
}

// ── Agent turn middleware ────────────────────────────────────────────────────

// AgentTurnStart begins a traced agent turn. Returns a context with the span
// and a function to call when the turn finishes.
func (h *Hub) AgentTurnStart(ctx context.Context, agentID, threadID string) (context.Context, func(error)) {
	h.mu.RLock()
	tracer := h.tracer
	h.mu.RUnlock()

	ctx, finish := tracer.StartSpan(ctx, "agent.turn",
		"agent_id", agentID,
		"thread_id", threadID,
	)
	start := time.Now()

	return ctx, func(turnErr error) {
		finish()
		duration := time.Since(start)
		if turnErr != nil {
			h.log.Warn("agent turn error", "agent", agentID, "thread", threadID,
				"error", turnErr, "duration_ms", duration.Milliseconds())
			h.mu.RLock()
			reporter := h.reporter
			h.mu.RUnlock()
			reporter.CaptureException(turnErr, map[string]string{
				"agent_id":    agentID,
				"thread_id":   threadID,
				"duration_ms": fmt.Sprint(duration.Milliseconds()),
			})
		}
	}
}

// ── Tool execution middleware ────────────────────────────────────────────────

// ToolExecStart begins a traced tool execution span.
func (h *Hub) ToolExecStart(ctx context.Context, toolName string) (context.Context, func(error)) {
	h.mu.RLock()
	tracer := h.tracer
	h.mu.RUnlock()

	ctx, finish := tracer.StartSpan(ctx, "tool.execute", "tool", toolName)
	start := time.Now()

	return ctx, func(execErr error) {
		finish()
		duration := time.Since(start)
		if execErr != nil {
			h.log.Debug("tool execution error", "tool", toolName,
				"error", execErr, "duration_ms", duration.Milliseconds())
		}
	}
}

// ── Convenience methods ─────────────────────────────────────────────────────

// CaptureError sends an error to the reporter.
func (h *Hub) CaptureError(err error, tags map[string]string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	h.reporter.CaptureException(err, tags)
}

// CaptureMessage sends a message to the reporter.
func (h *Hub) CaptureMessage(msg string, level string, tags map[string]string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	h.reporter.CaptureMessage(msg, level, tags)
}

// Flush drains pending events.
func (h *Hub) Flush(timeout time.Duration) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.reporter.Flush(timeout)
}

// EnableFileReporter creates a file-backed error reporter and sets it as the
// active reporter. Returns an error if the log file cannot be created.
func (h *Hub) EnableFileReporter(logsDir string) error {
	r, err := NewFileReporter(logsDir)
	if err != nil {
		return fmt.Errorf("enable file reporter: %w", err)
	}
	h.SetReporter(r)
	return nil
}

// InitFromConfig creates and configures an observability Hub from application
// settings. Enables file-based error reporting under the workspace logs directory.
// Returns a Hub and a logger that writes WARN+ records to both stderr and the
// workspace logs file.
func InitFromConfig(sentryDSN string, tracingEnabled bool, logLevel string, workspaceRoot string, log *slog.Logger) *Hub {
	logsDir := filepath.Join(workspaceRoot, "logs")

	// Open the log file for WARN+ records.
	fileWriter, err := openLogFile(logsDir)
	if err != nil && log != nil {
		log.Warn("observability: cannot open log file", "error", err)
	}

	// Build a logger that writes to both stderr (text, all levels) and the
	// log file (JSON, WARN+ only).
	var combinedLogger *slog.Logger
	if fileWriter != nil {
		stderrHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLogLevel(logLevel)})
		fileHandler := newFileHandler(fileWriter, slog.LevelWarn)
		combinedLogger = slog.New(newMultiHandler(stderrHandler, fileHandler))
	} else if log != nil {
		combinedLogger = log
	}

	hub := NewHub(nil, nil, combinedLogger)
	if fileWriter != nil {
		hub.fileWriter = fileWriter
	}

	if err := hub.EnableFileReporter(logsDir); err != nil && log != nil {
		log.Warn("observability: file reporter unavailable", "error", err)
	}

	_ = sentryDSN      // future: swap FileReporter for real Sentry SDK reporter
	_ = tracingEnabled // future: swap noopTracer for OTel tracer

	return hub
}

func openLogFile(logsDir string) (io.WriteCloser, error) {
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(logsDir, "app.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
}

// ── Multi-handler ────────────────────────────────────────────────────────

type multiHandler struct {
	handlers []slog.Handler
}

func newMultiHandler(handlers ...slog.Handler) *multiHandler {
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := &multiHandler{handlers: make([]slog.Handler, len(m.handlers))}
	for i, h := range m.handlers {
		clone.handlers[i] = h.WithAttrs(attrs)
	}
	return clone
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	clone := &multiHandler{handlers: make([]slog.Handler, len(m.handlers))}
	for i, h := range m.handlers {
		clone.handlers[i] = h.WithGroup(name)
	}
	return clone
}

// ── File handler (JSONL for WARN+) ───────────────────────────────────────

type fileHandler struct {
	writer io.Writer
	level  slog.Level
	attrs  []slog.Attr
	groups []string
}

func newFileHandler(w io.Writer, level slog.Level) *fileHandler {
	return &fileHandler{writer: w, level: level}
}

func (h *fileHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *fileHandler) Handle(_ context.Context, r slog.Record) error {
	m := map[string]interface{}{
		"time":  r.Time.UTC().Format(time.RFC3339),
		"level": r.Level.String(),
		"msg":   r.Message,
	}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.String()
		return true
	})
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = h.writer.Write(data)
	return err
}

func (h *fileHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(clone.attrs, attrs...)
	return &clone
}

func (h *fileHandler) WithGroup(name string) slog.Handler {
	clone := *h
	clone.groups = append(clone.groups, name)
	return &clone
}

func (h *fileHandler) Source() (*slog.Source, bool) {
	return &slog.Source{Function: "", File: "", Line: 0}, false
}

// Close tears down the observability hub.
func (h *Hub) Close() {
	h.Flush(5 * time.Second)
	h.mu.RLock()
	defer h.mu.RUnlock()
	h.reporter.Close()
	if h.fileWriter != nil {
		h.fileWriter.Close()
	}
}

// ── No-op implementations ────────────────────────────────────────────────────

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type noopReporter struct{}

func (n *noopReporter) CaptureException(err error, tags map[string]string)              {}
func (n *noopReporter) CaptureMessage(msg string, level string, tags map[string]string) {}
func (n *noopReporter) Flush(timeout time.Duration) bool                                { return true }
func (n *noopReporter) Close()                                                          {}

type noopTracer struct{}

func (n *noopTracer) StartSpan(ctx context.Context, name string, attrs ...string) (context.Context, func()) {
	return ctx, func() {}
}

// ── Host metadata ───────────────────────────────────────────────────────────

// HostInfo returns basic host metadata for error context.
func HostInfo() map[string]string {
	host, _ := os.Hostname()
	return map[string]string{
		"hostname": host,
		"goos":     os.Getenv("GOOS"),
		"goarch":   os.Getenv("GOARCH"),
	}
}
