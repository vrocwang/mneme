package boot

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/simon/mneme/internal/desktop"
	"github.com/simon/mneme/pkg/events"
)

type ingestCall struct {
	source  string
	content string
	taint   string
}

type fakeIngester struct {
	mu    sync.Mutex
	calls []ingestCall
	err   error
}

func (f *fakeIngester) IndexContentWithTaint(source, content, taint string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ingestCall{source: source, content: content, taint: taint})
	return f.err
}

func (f *fakeIngester) recorded() []ingestCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ingestCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// newSinkFixture builds a sink over a temp data dir, a buffer-backed logger and
// a bus subscription that captures desktop events.
func newSinkFixture(t *testing.T, ingester MemoryIngester) (*ScreenIntelSink, *bytes.Buffer, <-chan events.Event, *events.Bus) {
	t.Helper()
	dir := t.TempDir()
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	bus := events.NewBus()
	got := make(chan events.Event, 4)
	bus.SubscribeDomain(func(e events.Event) { got <- e }, events.DomainDesktop)
	return NewScreenIntelSink(logger, dir, ingester, bus), &logBuf, got, bus
}

func readObservationLines(t *testing.T, sink *ScreenIntelSink) []desktop.ScreenCaptureResult {
	t.Helper()
	data, err := os.ReadFile(sink.Path())
	if err != nil {
		t.Fatalf("read observation log: %v", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "\n")
	out := make([]desktop.ScreenCaptureResult, 0, len(parts))
	for i, line := range parts {
		var res desktop.ScreenCaptureResult
		if err := json.Unmarshal([]byte(line), &res); err != nil {
			t.Fatalf("line %d is not valid JSON (%q): %v", i+1, line, err)
		}
		out = append(out, res)
	}
	return out
}

// TestScreenIntelSink_Observations is the table-driven regression for the sink:
// every observation becomes exactly one JSON line; only a successful summary
// reaches memory, and always with the external taint; failures are logged.
func TestScreenIntelSink_Observations(t *testing.T) {
	ts := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cases := []struct {
		name       string
		result     desktop.ScreenCaptureResult
		ingestErr  error
		wantIngest []ingestCall
		wantLog    string
	}{
		{
			name: "summary is ingested as external content",
			result: desktop.ScreenCaptureResult{
				ActiveWindow: "Terminal", ScreenshotPath: "/tmp/shot.png",
				Summary: "A build is running", Timestamp: ts,
			},
			wantIngest: []ingestCall{{source: "screen_intel", content: "A build is running", taint: "external"}},
		},
		{
			name: "empty summary is not ingested",
			result: desktop.ScreenCaptureResult{
				ActiveWindow: "Finder", ScreenshotPath: "/tmp/shot.png",
				Summary: "   ", Timestamp: ts,
			},
		},
		{
			name: "error observation is recorded and never ingested",
			result: desktop.ScreenCaptureResult{
				ActiveWindow: "Safari", Error: "capture: screen recording not permitted",
				Summary: "should not be trusted", Timestamp: ts,
			},
			wantLog: "observation failed",
		},
		{
			name: "pipeline failure is logged",
			result: desktop.ScreenCaptureResult{
				ActiveWindow: "Notes", ScreenshotPath: "/tmp/shot.png",
				Summary: "Meeting notes", Timestamp: ts,
			},
			ingestErr:  errors.New("embedding unavailable"),
			wantIngest: []ingestCall{{source: "screen_intel", content: "Meeting notes", taint: "external"}},
			wantLog:    "memory ingest failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ing := &fakeIngester{err: tc.ingestErr}
			sink, logBuf, eventCh, _ := newSinkFixture(t, ing)

			sink.Handle(tc.result) // must not panic, even when the pipeline fails

			lines := readObservationLines(t, sink)
			if len(lines) != 1 {
				t.Fatalf("wrote %d observation lines, want 1", len(lines))
			}
			got := lines[0]
			if got.ActiveWindow != tc.result.ActiveWindow ||
				got.ScreenshotPath != tc.result.ScreenshotPath ||
				got.Summary != tc.result.Summary ||
				got.Error != tc.result.Error {
				t.Errorf("round-tripped observation = %+v, want %+v", got, tc.result)
			}
			if !got.Timestamp.Equal(ts) {
				t.Errorf("timestamp = %v, want %v", got.Timestamp, ts)
			}

			calls := ing.recorded()
			if len(calls) != len(tc.wantIngest) {
				t.Fatalf("ingest calls = %+v, want %+v", calls, tc.wantIngest)
			}
			for i := range calls {
				if calls[i] != tc.wantIngest[i] {
					t.Errorf("ingest call %d = %+v, want %+v", i, calls[i], tc.wantIngest[i])
				}
			}

			if tc.wantLog != "" && !strings.Contains(logBuf.String(), tc.wantLog) {
				t.Errorf("log does not mention %q:\n%s", tc.wantLog, logBuf.String())
			}

			select {
			case e := <-eventCh:
				if e.Domain != events.DomainDesktop || e.Kind != events.KindScreenIntel {
					t.Fatalf("event = %s/%s, want %s/%s", e.Domain, e.Kind, events.DomainDesktop, events.KindScreenIntel)
				}
				payload, ok := e.Data.(desktop.ScreenCaptureResult)
				if !ok {
					t.Fatalf("event payload has type %T, want desktop.ScreenCaptureResult", e.Data)
				}
				if payload.ActiveWindow != tc.result.ActiveWindow || payload.Error != tc.result.Error {
					t.Errorf("event payload = %+v, want %+v", payload, tc.result)
				}
			case <-time.After(time.Second):
				t.Fatal("no desktop/screen.intel event published")
			}
		})
	}
}

// TestScreenIntelSink_AppendsOneLinePerObservation guards the append-only log
// shape: successive observations must accumulate, not overwrite.
func TestScreenIntelSink_AppendsOneLinePerObservation(t *testing.T) {
	sink, _, _, _ := newSinkFixture(t, &fakeIngester{})
	sink.Handle(desktop.ScreenCaptureResult{ActiveWindow: "one", Timestamp: time.Now()})
	sink.Handle(desktop.ScreenCaptureResult{ActiveWindow: "two", Timestamp: time.Now()})

	lines := readObservationLines(t, sink)
	if len(lines) != 2 {
		t.Fatalf("wrote %d observation lines, want 2", len(lines))
	}
	if lines[0].ActiveWindow != "one" || lines[1].ActiveWindow != "two" {
		t.Errorf("lines = %+v, want [one two]", lines)
	}
	data, err := os.ReadFile(sink.Path())
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Error("observation log does not end with a newline")
	}
}

// TestScreenIntelSink_CreatesDataDir verifies the sink creates its directory
// (the GUI passes workspace/data, which may not exist yet).
func TestScreenIntelSink_CreatesDataDir(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "nested", "data")
	sink := NewScreenIntelSink(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), dataDir, nil, nil)

	sink.Handle(desktop.ScreenCaptureResult{ActiveWindow: "x", Timestamp: time.Now()})
	if _, err := os.Stat(filepath.Join(dataDir, ScreenIntelFileName)); err != nil {
		t.Fatalf("observation log not created: %v", err)
	}
}

// TestScreenIntelSink_NilPipelineAndBus ensures a sink wired without a memory
// pipeline or bus (e.g. pipeline disabled) still records observations.
func TestScreenIntelSink_NilPipelineAndBus(t *testing.T) {
	sink, logBuf, _, _ := newSinkFixture(t, nil)
	sink.Handle(desktop.ScreenCaptureResult{ActiveWindow: "x", Summary: "hello", Timestamp: time.Now()})
	if lines := readObservationLines(t, sink); len(lines) != 1 {
		t.Fatalf("wrote %d lines, want 1", len(lines))
	}
	if logBuf.Len() != 0 {
		t.Errorf("unexpected log output: %s", logBuf.String())
	}
}

// The capture loop can run for weeks; the log must not grow without bound.
func TestScreenIntelSink_RotatesOversizedLog(t *testing.T) {
	sink, _, _, _ := newSinkFixture(t, &fakeIngester{})

	// A single observation larger than the cap forces one rotation.
	big := desktop.ScreenCaptureResult{
		ActiveWindow: "editor",
		Summary:      strings.Repeat("x", ScreenIntelMaxBytes+1),
	}
	sink.Handle(big)
	if info, err := os.Stat(sink.Path()); err != nil || info.Size() == 0 {
		t.Fatalf("first observation must be written even when it exceeds the cap: %v", err)
	}

	// The next observation sees an oversized file and rotates it aside.
	sink.Handle(desktop.ScreenCaptureResult{ActiveWindow: "terminal", Summary: "small"})

	backup := sink.Path() + ".1"
	info, err := os.Stat(backup)
	if err != nil {
		t.Fatalf("expected a rotated backup at %s: %v", backup, err)
	}
	if info.Size() <= ScreenIntelMaxBytes {
		t.Fatalf("backup holds %d bytes; it should hold the oversized previous log", info.Size())
	}
	fresh, err := os.Stat(sink.Path())
	if err != nil {
		t.Fatalf("fresh log missing: %v", err)
	}
	if fresh.Size() > int64(ScreenIntelMaxBytes) {
		t.Fatalf("fresh log is %d bytes, above the %d cap", fresh.Size(), ScreenIntelMaxBytes)
	}
	if lines := readObservationLines(t, sink); len(lines) != 1 || lines[0].ActiveWindow != "terminal" {
		t.Fatalf("fresh log should hold exactly the newest observation, got %+v", lines)
	}

	// A second rotation replaces the backup instead of growing the directory.
	sink.Handle(big)
	sink.Handle(desktop.ScreenCaptureResult{ActiveWindow: "browser"})
	if entries, err := os.ReadDir(filepath.Dir(sink.Path())); err == nil {
		var backups int
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".1") {
				backups++
			}
		}
		if backups != 1 {
			t.Fatalf("found %d rotated backups, want exactly 1", backups)
		}
	}
}

// A runaway vision summary must not push an unbounded blob into memory.
func TestScreenIntelSink_TruncatesHugeSummaryBeforeIngest(t *testing.T) {
	ing := &fakeIngester{}
	sink, _, _, _ := newSinkFixture(t, ing)

	huge := strings.Repeat("s", screenIntelMaxIngestBytes*3)
	sink.Handle(desktop.ScreenCaptureResult{ActiveWindow: "editor", Summary: huge})

	calls := ing.recorded()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one ingest, got %d", len(calls))
	}
	if len(calls[0].content) > screenIntelMaxIngestBytes {
		t.Fatalf("ingested %d bytes, want at most %d", len(calls[0].content), screenIntelMaxIngestBytes)
	}
	if calls[0].taint != "external" {
		t.Fatalf("taint = %q, want external", calls[0].taint)
	}
}

// The ingest cap must not hand a half-encoded rune to the pipeline.
func TestScreenIntelSink_TruncatesAtRuneBoundary(t *testing.T) {
	ing := &fakeIngester{}
	sink, _, _, _ := newSinkFixture(t, ing)

	// Multi-byte runes across the cap: the naive byte slice would end mid-rune.
	sink.Handle(desktop.ScreenCaptureResult{Summary: strings.Repeat("屏", screenIntelMaxIngestBytes)})

	calls := ing.recorded()
	if len(calls) != 1 {
		t.Fatalf("expected one ingest, got %d", len(calls))
	}
	if !utf8.ValidString(calls[0].content) {
		t.Fatalf("ingested content is not valid UTF-8 (%d bytes)", len(calls[0].content))
	}
	if len(calls[0].content) > screenIntelMaxIngestBytes {
		t.Fatalf("ingested %d bytes, cap is %d", len(calls[0].content), screenIntelMaxIngestBytes)
	}
}
