package monitor

import (
	"strings"
	"testing"
	"time"
)

// TestDefaultRunTimeout covers SET 1.5: the monitor_start tool default and the
// RPC fallback come from one constant, and runTimeout resolves them uniformly.
func TestDefaultRunTimeout(t *testing.T) {
	if DefaultRunTimeout != 5*time.Minute {
		t.Fatalf("DefaultRunTimeout = %s, want 5m", DefaultRunTimeout)
	}

	tests := []struct {
		name string
		secs int
		want time.Duration
	}{
		{"zero falls back", 0, DefaultRunTimeout},
		{"negative falls back", -1, DefaultRunTimeout},
		{"explicit seconds are honoured", 60, 60 * time.Second},
		{"explicit 300 equals the default", 300, DefaultRunTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runTimeout(tt.secs); got != tt.want {
				t.Errorf("runTimeout(%d) = %s, want %s", tt.secs, got, tt.want)
			}
		})
	}

	// The tool schema still documents the same number.
	tool := NewMonitorStartTool(nil)
	schema := tool.Schema()
	props, _ := schema.Parameters["properties"].(map[string]interface{})
	timeoutProp, _ := props["timeout_secs"].(map[string]interface{})
	desc, _ := timeoutProp["description"].(string)
	if desc == "" {
		t.Fatal("monitor_start timeout_secs description is missing")
	}
	const documented = "default: 300 = 5 minutes"
	if !strings.Contains(desc, documented) {
		t.Errorf("monitor_start timeout description %q no longer documents %q", desc, documented)
	}
}
