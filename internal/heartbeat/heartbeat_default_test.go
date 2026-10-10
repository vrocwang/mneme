package heartbeat

import (
	"testing"
	"time"
)

// TestDefaultInterval covers SET 1.4: the zero-value fallback and the exported
// default are the same value.
func TestDefaultInterval(t *testing.T) {
	if DefaultInterval != 30*time.Second {
		t.Fatalf("DefaultInterval = %s, want 30s", DefaultInterval)
	}

	if got := New(nil, 0).interval; got != DefaultInterval {
		t.Errorf("New(_, 0).interval = %s, want DefaultInterval (%s)", got, DefaultInterval)
	}
	if got := New(nil, 5*time.Second).interval; got != 5*time.Second {
		t.Errorf("New(_, 5s).interval = %s, want 5s", got)
	}
}
