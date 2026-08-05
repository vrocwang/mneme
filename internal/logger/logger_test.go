package logger

import (
	"bytes"
	"log/slog"
	"testing"
)

func TestNewLogger_WritesStructuredJSON(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, slog.LevelDebug)
	log.Info("test message", "key", "value")

	output := buf.String()
	if output == "" {
		t.Fatal("expected log output")
	}
	if !contains(output, "test message") || !contains(output, `"key":"value"`) {
		t.Errorf("unexpected output: %s", output)
	}
}

func TestNewLogger_DiscardsBelowLevel(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, slog.LevelInfo)
	log.Debug("should be discarded")

	if buf.String() != "" {
		t.Error("expected empty buffer for discarded debug log")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
