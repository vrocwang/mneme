package bundle

import (
	"testing"
	"time"

	"github.com/simon/mneme/internal/heartbeat"
)

// TestHeartbeatIntervalAlias covers SET 1.4: the cadence the boot layer wires
// is the same value the heartbeat engine falls back to for a zero interval.
func TestHeartbeatIntervalAlias(t *testing.T) {
	if HeartbeatInterval != heartbeat.DefaultInterval {
		t.Errorf("bundle.HeartbeatInterval = %s, want heartbeat.DefaultInterval (%s)",
			HeartbeatInterval, heartbeat.DefaultInterval)
	}
	if HeartbeatInterval != 30*time.Second {
		t.Errorf("bundle.HeartbeatInterval = %s, want 30s", HeartbeatInterval)
	}
}
