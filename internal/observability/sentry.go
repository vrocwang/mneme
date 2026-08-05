package observability

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileReporter implements ErrorReporter by writing structured error events
// to a local JSONL file. This is an always-available backend that doesn't
// require network access. Swap in a real Sentry SDK reporter when the DSN
// is configured and the SDK dependency is available.
type FileReporter struct {
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
}

// NewFileReporter creates a file-backed error reporter.
func NewFileReporter(logsDir string) (*FileReporter, error) {
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return nil, fmt.Errorf("observability: create logs dir: %w", err)
	}
	path := filepath.Join(logsDir, "errors.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("observability: open error log: %w", err)
	}
	return &FileReporter{file: f, encoder: json.NewEncoder(f)}, nil
}

type errorEvent struct {
	Timestamp  string            `json:"timestamp"`
	Type       string            `json:"type"` // "exception" or "message"
	Message    string            `json:"message"`
	Level      string            `json:"level"`
	Tags       map[string]string `json:"tags,omitempty"`
	StackTrace string            `json:"stack_trace,omitempty"`
}

func (r *FileReporter) CaptureException(err error, tags map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.encoder == nil {
		return
	}
	r.encoder.Encode(errorEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Type:      "exception",
		Message:   err.Error(),
		Level:     "error",
		Tags:      tags,
	})
}

func (r *FileReporter) CaptureMessage(msg string, level string, tags map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.encoder == nil {
		return
	}
	r.encoder.Encode(errorEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Type:      "message",
		Message:   msg,
		Level:     level,
		Tags:      tags,
	})
}

func (r *FileReporter) Flush(timeout time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		r.file.Sync()
	}
	return true
}

func (r *FileReporter) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		r.file.Sync()
		r.file.Close()
		r.file = nil
	}
}
