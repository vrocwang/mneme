package tools

import (
	"os"
	"testing"

	pkgtools "github.com/simon/mneme/pkg/tools"
)

// TestDefaultTimeoutSecsAlias is the SET 1.2 guard: internal/tools re-exports
// the pkg/tools value instead of restating the number.
func TestDefaultTimeoutSecsAlias(t *testing.T) {
	if pkgtools.DefaultTimeoutSecs != 120 {
		t.Fatalf("pkgtools.DefaultTimeoutSecs = %d, want 120", pkgtools.DefaultTimeoutSecs)
	}
	if DefaultTimeoutSecs != pkgtools.DefaultTimeoutSecs {
		t.Errorf("tools.DefaultTimeoutSecs = %d, want the pkg/tools value %d",
			DefaultTimeoutSecs, pkgtools.DefaultTimeoutSecs)
	}
	if MinTimeoutSecs > DefaultTimeoutSecs || DefaultTimeoutSecs > MaxTimeoutSecs {
		t.Errorf("default timeout %d outside [%d, %d]", DefaultTimeoutSecs, MinTimeoutSecs, MaxTimeoutSecs)
	}

	// The runtime global must start at the same value unless the operator set
	// the env override.
	if os.Getenv(TimeoutEnvVar) == "" && ToolTimeoutSecs() != DefaultTimeoutSecs {
		t.Errorf("ToolTimeoutSecs() = %d, want the default %d", ToolTimeoutSecs(), DefaultTimeoutSecs)
	}
}
