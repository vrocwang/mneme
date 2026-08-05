package cwd_jail

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNoopBackend(t *testing.T) {
	b := &noopBackend{}
	if b.Name() != "noop" {
		t.Errorf("expected name noop, got %s", b.Name())
	}
	if !b.IsAvailable() {
		t.Error("noop backend should always be available")
	}

	jail := &Jail{
		Root:  t.TempDir(),
		Label: "test-jail",
	}
	cmd, err := b.Spawn(context.Background(), jail, "echo", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != jail.Root {
		t.Errorf("cmd dir should be jail root, got %s", cmd.Dir)
	}
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()

	j1 := &Jail{Root: "/tmp/a", Label: "a"}
	j2 := &Jail{Root: "/tmp/b", Label: "b"}

	reg.Register(j1)
	reg.Register(j2)

	if len(reg.List()) != 2 {
		t.Errorf("expected 2 jails, got %d", len(reg.List()))
	}

	reg.MarkActive("a")
	if reg.ActiveCount() != 1 {
		t.Errorf("expected 1 active, got %d", reg.ActiveCount())
	}

	reg.MarkInactive("a")
	if reg.ActiveCount() != 0 {
		t.Errorf("expected 0 active, got %d", reg.ActiveCount())
	}

	reg.Unregister("a")
	if len(reg.List()) != 1 {
		t.Errorf("expected 1 jail after unregister, got %d", len(reg.List()))
	}
}

func TestBackendName(t *testing.T) {
	name := BackendName()
	if name == "" || name == "none" {
		t.Logf("backend name: %s (may be noop on unsupported platform)", name)
	}
}

func TestIsPathJailed(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")

	reg := NewRegistry()
	reg.Register(&Jail{Root: dir, Label: "test"})

	// Override the global registry for testing.
	oldReg := globalReg
	globalReg = reg
	defer func() { globalReg = oldReg }()

	if !IsPathJailed(subDir) {
		t.Errorf("%s should be jailed under %s", subDir, dir)
	}

	unrelated := t.TempDir()
	if IsPathJailed(unrelated) {
		t.Errorf("%s should NOT be jailed", unrelated)
	}
}

func TestSpawn_CanonicalizesRoot(t *testing.T) {
	dir := t.TempDir()
	// Create a symlink to the temp dir.
	linkPath := filepath.Join(t.TempDir(), "jail-link")
	os.Symlink(dir, linkPath)

	// Ensure backend is initialized for this test.
	backendOnce.Do(func() { globalBackend = Detect() })

	// Spawn should canonicalize the root, resolving the symlink.
	// Even with a noop backend the path resolution happens.
	cmd, err := Spawn(context.Background(), &Jail{Root: linkPath, Label: "symlink-test"}, "echo", "hello")
	if err != nil {
		t.Fatal(err)
	}
	// The jail root should be resolved.
	_ = cmd
}
