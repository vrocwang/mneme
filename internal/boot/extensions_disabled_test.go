package boot

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/config"
)

// TestBootstrapAllThreadsBundlesDisabledIntoExtensions guards the deleted
// gates: after the core→extension migration, `[bundles] disabled` must still
// reach the runtime extension loader, or `disabled = ["todos"]` (file-state,
// workflows) silently does nothing.
//
// The fixture extension has no binary and no go.mod, so nothing is launched.
// The observable difference is where discovery stops: a disabled extension is
// skipped by name before its binary is ever resolved, while an enabled one
// falls through to the "binary not found" warning.
func TestBootstrapAllThreadsBundlesDisabledIntoExtensions(t *testing.T) {
	workspace := t.TempDir()
	extDir := filepath.Join(workspace, "extensions", "todos")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := `{"name":"todos","version":"0.1.0","category":"tools","binary":"todos-binary-absent"}`
	if err := os.WriteFile(filepath.Join(extDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	run := func(disabled []string) string {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

		reg := capability.NewCapabilityRegistry()
		defer reg.Shutdown()

		cfg := &config.Config{Workspace: workspace}
		cfg.Bundles.Disabled = disabled

		BootstrapAll(reg, workspace, "standard", nil, nil, cfg, nil, nil, log)

		if _, ok := reg.GetSet(capability.ExtensionSetID("todos")); ok {
			t.Errorf("extension:todos must never register with an absent binary")
		}
		return buf.String()
	}

	disabledLog := run([]string{"todos"})
	if !strings.Contains(disabledLog, "extension disabled by config") {
		t.Errorf("cfg.Bundles.Disabled did not reach the extension loader; log:\n%s", disabledLog)
	}
	if strings.Contains(disabledLog, "extension binary not found") {
		t.Errorf("a disabled extension must be skipped before its binary is resolved; log:\n%s", disabledLog)
	}

	enabledLog := run(nil)
	if strings.Contains(enabledLog, "extension disabled by config") {
		t.Errorf("no disable entry, but discovery skipped the extension; log:\n%s", enabledLog)
	}
	if !strings.Contains(enabledLog, "extension binary not found") {
		t.Errorf("control: an enabled extension must reach binary resolution; log:\n%s", enabledLog)
	}
}
