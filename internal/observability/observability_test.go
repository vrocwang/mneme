package observability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The live purpose of this package is workspace file logging: verify that a
// WARN record reaches logs/app.jsonl through the multi-handler.
func TestInitFromConfigWritesWarnToLogFile(t *testing.T) {
	ws := t.TempDir()
	hub := InitFromConfig("warn", ws, nil)
	hub.Logger().Warn("hello-observability")
	hub.Close()

	data, err := os.ReadFile(filepath.Join(ws, "logs", "app.jsonl"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "hello-observability") {
		t.Errorf("log file missing WARN record: %q", string(data))
	}
}

// The file handler is WARN+ only, regardless of the stderr level.
func TestInitFromConfigFileHandlerSkipsDebug(t *testing.T) {
	ws := t.TempDir()
	hub := InitFromConfig("debug", ws, nil)
	hub.Logger().Debug("debug-only-record")
	hub.Close()

	data, err := os.ReadFile(filepath.Join(ws, "logs", "app.jsonl"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if strings.Contains(string(data), "debug-only-record") {
		t.Errorf("DEBUG record must not be written to the file: %q", string(data))
	}
}

// Close must be safe when no log file could be opened.
func TestHubCloseWithoutFileWriter(t *testing.T) {
	h := newHub(nil)
	if h.Logger() == nil {
		t.Fatal("newHub must always provide a logger")
	}
	h.Close() // must not panic
}
