package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFileAt creates a file and forces its modification time, so pruning's
// newest-first ordering is deterministic without a real capture or fake clock.
func writeFileAt(t *testing.T, path string, mod time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// TestPruneScreenshotsKeepsNewestAndIgnoresOtherFiles pins the two guarantees
// that matter: only the newest MaxScreenshots-style window survives, and
// nothing outside the screenshot naming pattern is ever deleted.
func TestPruneScreenshotsKeepsNewestAndIgnoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	base := time.Unix(1_700_000_000, 0)

	const total = 10
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("screenshot-%d.png", base.Unix()+int64(i))
		writeFileAt(t, filepath.Join(dir, name), base.Add(time.Duration(i)*time.Second))
	}
	// Decoys: unrelated files and names that merely resemble the pattern.
	decoys := []string{"notes.txt", "screenshot-abc.png", "screenshot-1.png.bak", "screenshot-1.png "}
	for _, name := range decoys {
		writeFileAt(t, filepath.Join(dir, name), base)
	}
	// A directory whose name matches the pattern must be skipped too.
	if err := os.Mkdir(filepath.Join(dir, "screenshot-1234.png"), 0o755); err != nil {
		t.Fatalf("mkdir decoy: %v", err)
	}

	deleted, err := pruneScreenshots(dir, 4)
	if err != nil {
		t.Fatalf("pruneScreenshots: %v", err)
	}
	if deleted != total-4 {
		t.Fatalf("deleted = %d, want %d", deleted, total-4)
	}

	for i := 0; i < total; i++ {
		name := fmt.Sprintf("screenshot-%d.png", base.Unix()+int64(i))
		path := filepath.Join(dir, name)
		_, statErr := os.Stat(path)
		if i < total-4 {
			if !os.IsNotExist(statErr) {
				t.Errorf("old screenshot %s survived pruning (stat err=%v)", name, statErr)
			}
			continue
		}
		if statErr != nil {
			t.Errorf("newest screenshot %s was pruned: %v", name, statErr)
		}
	}

	for _, name := range decoys {
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr != nil {
			t.Errorf("unrelated file %q was deleted: %v", name, statErr)
		}
	}
	if info, statErr := os.Stat(filepath.Join(dir, "screenshot-1234.png")); statErr != nil || !info.IsDir() {
		t.Errorf("directory named like a screenshot was touched: info=%v err=%v", info, statErr)
	}
}

// TestPruneScreenshotsNoOpAtOrUnderKeep verifies pruning is a no-op while the
// directory is within budget.
func TestPruneScreenshotsNoOpAtOrUnderKeep(t *testing.T) {
	dir := t.TempDir()
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("screenshot-%d.png", base.Unix()+int64(i))
		writeFileAt(t, filepath.Join(dir, name), base.Add(time.Duration(i)*time.Second))
	}

	deleted, err := pruneScreenshots(dir, 3)
	if err != nil {
		t.Fatalf("pruneScreenshots: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d at exactly keep, want 0", deleted)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("directory has %d entries, want 3", len(entries))
	}
}

// TestPruneScreenshotsMissingDirReportsError keeps the capture path's contract
// explicit: a bad directory surfaces an error for the caller to log.
func TestPruneScreenshotsMissingDirReportsError(t *testing.T) {
	if _, err := pruneScreenshots(filepath.Join(t.TempDir(), "does-not-exist"), 1); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}
