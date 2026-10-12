package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolvePathUsesWorkspaceRoot pins the resolution rule: relative tool
// arguments are workspace-relative (MNEME_HOME), absolute ones pass through.
func TestResolvePathUsesWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MNEME_HOME", home)

	if got, want := resolvePath("sub/dir"), filepath.Join(home, "sub", "dir"); got != want {
		t.Errorf("resolvePath(relative) = %q, want %q", got, want)
	}
	abs := filepath.Join(home, "elsewhere")
	if got := resolvePath(abs); got != abs {
		t.Errorf("resolvePath(absolute) = %q, want %q", got, abs)
	}
	if got := resolvePath(""); got != "" {
		t.Errorf("resolvePath(empty) = %q, want empty", got)
	}
}

// TestFileToolsResolveRelativePathsAgainstWorkspace drives both tools with a
// relative path and a temp MNEME_HOME. The host's policy check resolves
// path-like args against the workspace root, so the extension must too;
// resolving against the process CWD (the old filepath.Abs behaviour) would read
// a different directory than the one that was authorised.
func TestFileToolsResolveRelativePathsAgainstWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MNEME_HOME", home)

	sub := filepath.Join(home, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}

	tools := &fileTools{tracker: NewTracker()}

	// Guard against a vacuous test: the raw tracker resolves "sub" against the
	// process CWD (the old behaviour), which must NOT be the workspace here —
	// that mismatch is exactly what the handlers' resolvePath fixes.
	raw := NewTracker()
	if _, err := raw.TakeSnapshot("sub"); err != nil {
		t.Fatalf("raw snapshot: %v", err)
	}
	if raw.Current().Root == sub {
		t.Fatalf("fixture is degenerate: the process CWD already is %q", sub)
	}

	res := tools.snapshot(context.Background(), map[string]interface{}{"path": "sub"})
	if res.Error != "" {
		t.Fatalf("snapshot(relative) failed: %s", res.Error)
	}
	snap := tools.tracker.Current()
	if snap == nil {
		t.Fatal("no snapshot recorded")
	}
	if snap.Root != sub {
		t.Fatalf("snapshot root = %q, want the workspace-relative %q", snap.Root, sub)
	}
	if _, ok := snap.Files["a.txt"]; !ok {
		t.Fatalf("snapshot did not see a.txt: %v", snap.Files)
	}

	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}
	res = tools.diff(context.Background(), map[string]interface{}{"path": "sub"})
	if res.Error != "" {
		t.Fatalf("diff(relative) failed: %s", res.Error)
	}
	if !strings.Contains(res.Output, "b.txt") || !strings.Contains(res.Output, "created") {
		t.Fatalf("diff did not report the created b.txt: %s", res.Output)
	}
}

// TestFileToolsAbsolutePathStillWorks is the control for the resolution change.
func TestFileToolsAbsolutePathStillWorks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MNEME_HOME", home)

	tools := &fileTools{tracker: NewTracker()}
	res := tools.snapshot(context.Background(), map[string]interface{}{"path": home})
	if res.Error != "" {
		t.Fatalf("snapshot(absolute) failed: %s", res.Error)
	}
	if snap := tools.tracker.Current(); snap == nil || snap.Root != home {
		t.Fatalf("snapshot root = %+v, want %q", snap, home)
	}
}
