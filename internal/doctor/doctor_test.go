package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunDiagnostics_ChecksCommonPaths(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "config"), 0755)
	os.WriteFile(filepath.Join(dir, "config", "config.toml"), []byte{}, 0644)

	report := Run(dir)
	if len(report.Issues) != 0 {
		t.Logf("issues found: %v", report.Issues)
	}
	if report.WorkspacePath != dir {
		t.Errorf("expected workspace %s, got %s", dir, report.WorkspacePath)
	}
}

func TestRunDiagnostics_MissingWorkspace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	report := Run(dir)
	if len(report.Issues) == 0 {
		t.Error("expected issues for missing workspace")
	}
}
