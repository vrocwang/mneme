package file_state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTakeSnapshot(t *testing.T) {
	dir, cleanup := tempDir(t)
	defer cleanup()

	writeFile(t, dir, "hello.txt", "hello world")
	writeFile(t, dir, "sub/nested.txt", "nested file")

	tracker := NewTracker()
	snap, err := tracker.TakeSnapshot(dir)
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}

	if snap.Root != dir {
		t.Fatalf("expected root %s, got %s", dir, snap.Root)
	}
	if len(snap.Files) < 2 {
		t.Fatalf("expected at least 2 files, got %d", len(snap.Files))
	}
}

func TestDiffDetectsNewFiles(t *testing.T) {
	dir, cleanup := tempDir(t)
	defer cleanup()

	writeFile(t, dir, "a.txt", "original")
	tracker := NewTracker()
	tracker.TakeSnapshot(dir)
	time.Sleep(10 * time.Millisecond)
	writeFile(t, dir, "b.txt", "new file")

	changes, err := tracker.Diff(dir)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	found := false
	for _, c := range changes {
		if c.Path == "b.txt" && c.Change == ChangeCreated {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected b.txt to be created, got changes: %v", changes)
	}
}

func TestDiffDetectsModifiedFiles(t *testing.T) {
	dir, cleanup := tempDir(t)
	defer cleanup()

	writeFile(t, dir, "mod.txt", "v1")
	tracker := NewTracker()
	tracker.TakeSnapshot(dir)
	time.Sleep(10 * time.Millisecond)
	writeFile(t, dir, "mod.txt", "v2 modified")

	changes, err := tracker.Diff(dir)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	for _, c := range changes {
		if c.Path == "mod.txt" && c.Change == ChangeModified {
			return
		}
	}
	t.Fatalf("expected mod.txt to be modified: %v", changes)
}

func TestDiffDetectsDeletes(t *testing.T) {
	dir, cleanup := tempDir(t)
	defer cleanup()

	writeFile(t, dir, "del.txt", "to delete")
	tracker := NewTracker()
	tracker.TakeSnapshot(dir)
	time.Sleep(10 * time.Millisecond)
	os.Remove(filepath.Join(dir, "del.txt"))

	changes, err := tracker.Diff(dir)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	for _, c := range changes {
		if c.Path == "del.txt" && c.Change == ChangeDeleted {
			return
		}
	}
	t.Fatalf("expected del.txt to be deleted: %v", changes)
}

func TestListChangedPaths(t *testing.T) {
	dir, cleanup := tempDir(t)
	defer cleanup()

	writeFile(t, dir, "a.txt", "a")
	tracker := NewTracker()
	tracker.TakeSnapshot(dir)
	time.Sleep(10 * time.Millisecond)
	writeFile(t, dir, "new.txt", "new")

	paths, err := tracker.ListChangedPaths(dir)
	if err != nil {
		t.Fatalf("ListChangedPaths: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 change, got %d: %v", len(paths), paths)
	}
}

func TestReset(t *testing.T) {
	dir, cleanup := tempDir(t)
	defer cleanup()

	writeFile(t, dir, "x.txt", "x")
	tracker := NewTracker()
	tracker.TakeSnapshot(dir)
	tracker.Reset()

	if tracker.Current() != nil {
		t.Fatal("expected nil snapshot after reset")
	}
}

func TestShouldExclude(t *testing.T) {
	cases := []struct {
		path     string
		expected bool
	}{
		{".git/config", true},
		{"node_modules/pkg/index.js", true},
		{"target/debug/binary", true},
		{"src/main.go", false},
		{"README.md", false},
	}
	for _, tc := range cases {
		if ShouldExclude(tc.path, DefaultExcludePatterns) != tc.expected {
			t.Errorf("ShouldExclude(%q) = %v, want %v", tc.path, !tc.expected, tc.expected)
		}
	}
}

func tempDir(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "oh-fs-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	return dir, func() { os.RemoveAll(dir) }
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
