package boot

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/simon/mneme/internal/desktop"
	"github.com/simon/mneme/internal/memory"
	"github.com/simon/mneme/pkg/events"
)

// ScreenIntelFileName is the append-only observation log written under the
// workspace data directory.
const ScreenIntelFileName = "screen_intel.jsonl"

// ScreenIntelMaxBytes bounds the observation log. The capture loop can run
// every few seconds for weeks, so an unbounded append would quietly fill the
// disk; once the file would exceed this size it is rotated to
// "<name>.1" (replacing any previous backup) and started fresh, so at most
// 2×ScreenIntelMaxBytes is ever held on disk.
const ScreenIntelMaxBytes = 5 << 20 // 5 MiB

// screenIntelMaxIngestBytes bounds a single memory ingest. A vision model that
// answers with a wall of text must not be able to push an unbounded blob into
// the memory pipeline.
const screenIntelMaxIngestBytes = 8 << 10 // 8 KiB

// MemoryIngester is the slice of memory.Pipeline the screen-intel sink needs.
// Keeping it an interface lets the sink be tested without a database.
type MemoryIngester interface {
	IndexContentWithTaint(source, content, taint string) error
}

// ScreenIntelSink consumes observations from desktop.ScreenIntelLoop:
//
//  1. every observation is appended as one JSON line to
//     <workspace>/data/screen_intel.jsonl, errors included (an observation
//     carrying Error is still recorded, and logged, so a failed capture is
//     never indistinguishable from a successful one);
//  2. a non-empty Summary is ingested into memory as external content —
//     screen text is untrusted (a prompt-injection surface), so it must not
//     enter memory as trusted text;
//  3. a desktop/screen.intel bus event carries the observation to the UI.
//
// All failures are logged; Handle never panics.
type ScreenIntelSink struct {
	log      *slog.Logger
	path     string
	pipeline MemoryIngester
	bus      *events.Bus

	mu sync.Mutex // serializes file appends
}

// NewScreenIntelSink creates the sink. dataDir is the workspace data directory
// (workspace.DataDir); pipeline and bus may be nil.
func NewScreenIntelSink(log *slog.Logger, dataDir string, pipeline MemoryIngester, bus *events.Bus) *ScreenIntelSink {
	if log == nil {
		log = slog.Default()
	}
	return &ScreenIntelSink{
		log:      log,
		path:     filepath.Join(dataDir, ScreenIntelFileName),
		pipeline: pipeline,
		bus:      bus,
	}
}

// Path returns the observation log path (useful for diagnostics and tests).
func (s *ScreenIntelSink) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Handle records one screen intelligence observation.
func (s *ScreenIntelSink) Handle(result desktop.ScreenCaptureResult) {
	if s == nil {
		return
	}
	s.appendLine(result)

	if result.Error != "" {
		s.log.Warn("screen intel observation failed",
			"error", result.Error, "active_window", result.ActiveWindow)
	} else if summary := strings.TrimSpace(result.Summary); summary != "" {
		if len(summary) > screenIntelMaxIngestBytes {
			s.log.Warn("screen intel summary truncated before memory ingest",
				"bytes", len(summary), "limit", screenIntelMaxIngestBytes)
			summary = summary[:screenIntelMaxIngestBytes]
			// Do not hand a half-encoded rune to the pipeline.
			for len(summary) > 0 && !utf8.ValidString(summary) {
				summary = summary[:len(summary)-1]
			}
		}
		if s.pipeline != nil {
			// Untrusted source: taint "external" keeps screen text out of the
			// trusted memory tier (prompt-injection surface).
			if err := s.pipeline.IndexContentWithTaint("screen_intel", summary, "external"); err != nil {
				s.log.Error("screen intel memory ingest failed", "error", err)
			}
		}
	}

	if s.bus != nil {
		s.bus.PublishTyped(events.DomainDesktop, events.KindScreenIntel, result)
	}
}

// appendLine writes one JSON object per line. Failures are logged, not
// propagated: screen observation must never take the capture loop down.
func (s *ScreenIntelSink) appendLine(result desktop.ScreenCaptureResult) {
	line, err := json.Marshal(result)
	if err != nil {
		s.log.Error("screen intel marshal failed", "error", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			s.log.Error("screen intel log dir create failed", "dir", dir, "error", err)
			return
		}
	}
	s.rotateIfNeeded(len(line) + 1)

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		s.log.Error("screen intel log open failed", "path", s.path, "error", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		s.log.Error("screen intel log write failed", "path", s.path, "error", err)
	}
}

// rotateIfNeeded moves the log aside once it would exceed ScreenIntelMaxBytes.
// The caller holds s.mu. A rotation failure is logged and the write proceeds:
// losing the rotation is better than losing the observation.
func (s *ScreenIntelSink) rotateIfNeeded(incoming int) {
	info, err := os.Stat(s.path)
	if err != nil || info.Size()+int64(incoming) <= ScreenIntelMaxBytes {
		return
	}
	backup := s.path + ".1"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		s.log.Warn("screen intel log backup remove failed", "path", backup, "error", err)
	}
	if err := os.Rename(s.path, backup); err != nil {
		s.log.Error("screen intel log rotate failed", "path", s.path, "error", err)
		return
	}
	s.log.Info("screen intel log rotated", "path", s.path, "backup", backup, "bytes", info.Size())
}

// screenIntelIngester adapts a possibly-nil *memory.Pipeline to MemoryIngester.
// Passing a nil *Pipeline directly would produce a non-nil interface holding a
// nil pointer, and the first IndexContentWithTaint call would panic.
func screenIntelIngester(p *memory.Pipeline) MemoryIngester {
	if p == nil {
		return nil
	}
	return p
}
