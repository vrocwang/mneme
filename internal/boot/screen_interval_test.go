package boot

import (
	"testing"
	"time"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/desktop"
)

// The configured screen-capture cadence and the capture loop's zero-value
// fallback must agree: the config default is what gui.go passes, and a
// zero-valued config (explicit 0 in TOML) falls through to the loop's constant.
// Neither package may import the other, so this is the only place the drift can
// be caught.
func TestScreenCaptureIntervalDefaultMatchesLoopFallback(t *testing.T) {
	configured := time.Duration(config.DefaultScreenCaptureIntervalSecs) * time.Second
	if configured != desktop.DefaultCaptureInterval {
		t.Fatalf("config default = %s, desktop fallback = %s; make them equal",
			configured, desktop.DefaultCaptureInterval)
	}
	if configured <= 0 {
		t.Fatalf("screen capture interval must be positive, got %s", configured)
	}
}
