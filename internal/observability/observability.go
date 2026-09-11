// Package observability provides the application's workspace file logging:
// a slog logger that writes all records to stderr and WARN+ records as JSONL
// to <workspace>/logs/app.jsonl, plus the Hub that owns the log file handle.
//
// Remote error reporting (Sentry) and OpenTelemetry tracing are intentionally
// not implemented; the earlier no-op tracer/reporter interfaces were removed
// because nothing fed them. Reintroduce them together with a real backend.
package observability

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	workspace "github.com/simon/mneme/internal/workspace"
)

// Hub owns the workspace log file and exposes the configured logger.
type Hub struct {
	log        *slog.Logger
	fileWriter io.WriteCloser // app.jsonl log file (closed on shutdown)
}

// newHub creates an observability hub. A nil logger falls back to the default.
func newHub(log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{log: log.With("component", "observability")}
}

// Logger returns the hub's configured logger.
func (h *Hub) Logger() *slog.Logger { return h.log }

// Close flushes and closes the workspace log file.
func (h *Hub) Close() {
	if h.fileWriter != nil {
		h.fileWriter.Close()
	}
}

// InitFromConfig creates and configures an observability Hub from application
// settings. It returns a logger that writes to both stderr (text, all levels)
// and the workspace logs file (JSON, WARN+ only).
func InitFromConfig(logLevel string, workspaceRoot string, log *slog.Logger) *Hub {
	logsDir := workspace.LogsDir(workspaceRoot)

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

	hub := newHub(combinedLogger)
	if fileWriter != nil {
		hub.fileWriter = fileWriter
	}
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
