package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestShipAgentPacks covers item 4: agent packs (agent.toml + prompt.md, no Go
// code) must reach extensions-dist/agents/<name>/, non-packs must be ignored,
// already-built Go extensions must be left alone, and re-running must be
// idempotent.
func TestShipAgentPacks(t *testing.T) {
	root := t.TempDir()
	extDir := filepath.Join(root, "extensions")
	distDir := filepath.Join(root, "extensions-dist")

	pack := filepath.Join(extDir, "agents", "code-executor")
	if err := os.MkdirAll(filepath.Join(pack, "prompts"), 0o755); err != nil {
		t.Fatalf("mkdir pack: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pack, "agent.toml"), []byte("id = \"code-executor\"\n"), 0o644); err != nil {
		t.Fatalf("write agent.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pack, "prompt.md"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pack, "prompts", "extra.md"), []byte("nested"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	// A directory under agents/ without agent.toml is not a pack.
	if err := os.MkdirAll(filepath.Join(extDir, "agents", "not-a-pack"), 0o755); err != nil {
		t.Fatalf("mkdir non-pack: %v", err)
	}

	// A previously built Go extension must survive the pack copy.
	goExt := filepath.Join(distDir, "todos")
	if err := os.MkdirAll(goExt, 0o755); err != nil {
		t.Fatalf("mkdir go ext: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goExt, "todos"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write go ext: %v", err)
	}

	copied, failed := shipAgentPacks(extDir, distDir)
	if copied != 1 || failed != 0 {
		t.Fatalf("first run: copied=%d failed=%d, want 1/0", copied, failed)
	}
	for _, rel := range []string{"agent.toml", "prompt.md", filepath.Join("prompts", "extra.md")} {
		got, err := os.ReadFile(filepath.Join(distDir, "agents", "code-executor", rel))
		if err != nil {
			t.Fatalf("expected %s in dist: %v", rel, err)
		}
		if len(got) == 0 {
			t.Fatalf("%s copied empty", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(distDir, "agents", "not-a-pack")); !os.IsNotExist(err) {
		t.Errorf("non-pack directory was shipped: %v", err)
	}
	if _, err := os.Stat(filepath.Join(goExt, "todos")); err != nil {
		t.Errorf("existing Go extension removed by pack copy: %v", err)
	}

	// Idempotent, and stale files inside a pack's own dist dir are refreshed
	// away rather than accumulating.
	stale := filepath.Join(distDir, "agents", "code-executor", "stale.md")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	copied, failed = shipAgentPacks(extDir, distDir)
	if copied != 1 || failed != 0 {
		t.Fatalf("second run: copied=%d failed=%d, want 1/0", copied, failed)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file survived the re-copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(goExt, "todos")); err != nil {
		t.Errorf("existing Go extension removed by second pack copy: %v", err)
	}

	// No agents/ directory at all is not an error.
	copied, failed = shipAgentPacks(filepath.Join(root, "empty-extensions"), distDir)
	if copied != 0 || failed != 0 {
		t.Fatalf("missing agents dir: copied=%d failed=%d, want 0/0", copied, failed)
	}
}
