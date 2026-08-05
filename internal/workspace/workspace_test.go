package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrap_CreatesDefaultLayout(t *testing.T) {
	root := t.TempDir()

	if err := Bootstrap(root); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	expected := []string{
		"projects",
		"data",
		"memory",
		"config",
		"secrets",
		"logs",
		"screenshots",
	}
	for _, dir := range expected {
		path := filepath.Join(root, dir)
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Errorf("expected directory %s to exist", path)
		}
	}
}

func TestBootstrap_Idempotent(t *testing.T) {
	root := t.TempDir()

	if err := Bootstrap(root); err != nil {
		t.Fatalf("first bootstrap failed: %v", err)
	}
	if err := Bootstrap(root); err != nil {
		t.Fatalf("second bootstrap failed: %v", err)
	}
}
