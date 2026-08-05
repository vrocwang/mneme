// Package file_state tracks file changes, checksums, and snapshots for
// agent workspace awareness — knowing what files were created, modified, or
// deleted during a task.
package file_state

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ChangeType describes what happened to a file.
type ChangeType string

const (
	ChangeCreated  ChangeType = "created"
	ChangeModified ChangeType = "modified"
	ChangeDeleted  ChangeType = "deleted"
)

// FileChange records a single file change event.
type FileChange struct {
	Path      string     `json:"path"`
	Change    ChangeType `json:"change"`
	SizeBytes int64      `json:"size_bytes"`
	Checksum  string     `json:"checksum,omitempty"`
	ModTime   time.Time  `json:"mod_time"`
}

// Snapshot represents the state of a directory tree at a point in time.
type Snapshot struct {
	ID        string              `json:"id"`
	Root      string              `json:"root"`
	CreatedAt time.Time           `json:"created_at"`
	Files     map[string]FileInfo `json:"files"`
}

// FileInfo holds file metadata for snapshot comparison.
type FileInfo struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	Checksum string    `json:"checksum"`
	IsDir    bool      `json:"is_dir"`
}

// Tracker monitors a directory and can detect changes relative to a baseline.
type Tracker struct {
	mu       sync.RWMutex
	snapshot *Snapshot
}

// NewTracker creates a file state tracker.
func NewTracker() *Tracker {
	return &Tracker{}
}

// TakeSnapshot captures the current state of a directory tree.
func (t *Tracker) TakeSnapshot(root string) (*Snapshot, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}

	s := &Snapshot{
		ID:        fmt.Sprintf("snap_%d", time.Now().UnixMilli()),
		Root:      absRoot,
		CreatedAt: time.Now(),
		Files:     make(map[string]FileInfo),
	}

	err = filepath.Walk(absRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible files
		}
		relPath, _ := filepath.Rel(absRoot, path)
		fi := FileInfo{
			Path:    relPath,
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   info.IsDir(),
		}
		if !info.IsDir() {
			fi.Checksum = fileChecksum(path)
		}
		s.Files[relPath] = fi
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}

	t.mu.Lock()
	t.snapshot = s
	t.mu.Unlock()

	return s, nil
}

// Diff compares the current state against the last snapshot and returns changes.
func (t *Tracker) Diff(root string) ([]FileChange, error) {
	t.mu.RLock()
	baseline := t.snapshot
	t.mu.RUnlock()

	if baseline == nil {
		return nil, fmt.Errorf("no baseline snapshot; call TakeSnapshot first")
	}

	current, err := t.TakeSnapshot(root)
	if err != nil {
		return nil, err
	}

	var changes []FileChange

	// Find created and modified files
	for path, info := range current.Files {
		old, existed := baseline.Files[path]
		if !existed {
			changes = append(changes, FileChange{
				Path:      path,
				Change:    ChangeCreated,
				SizeBytes: info.Size,
				Checksum:  info.Checksum,
				ModTime:   info.ModTime,
			})
		} else if !info.IsDir && (info.Checksum != old.Checksum || info.Size != old.Size) {
			// Directories are skipped here: a directory's size/mtime changes
			// whenever entries are added or removed, which is already captured
			// by the individual file changes below. Reporting directories as
			// "modified" would only duplicate that signal as noise.
			changes = append(changes, FileChange{
				Path:      path,
				Change:    ChangeModified,
				SizeBytes: info.Size,
				Checksum:  info.Checksum,
				ModTime:   info.ModTime,
			})
		}
	}

	// Find deleted files
	for path, old := range baseline.Files {
		if _, exists := current.Files[path]; !exists {
			changes = append(changes, FileChange{
				Path:    path,
				Change:  ChangeDeleted,
				ModTime: old.ModTime,
			})
		}
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// Reset clears the current snapshot.
func (t *Tracker) Reset() {
	t.mu.Lock()
	t.snapshot = nil
	t.mu.Unlock()
}

// Current returns the current snapshot, or nil.
func (t *Tracker) Current() *Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.snapshot
}

// ListChangedPaths returns just the paths of files that changed since the baseline.
func (t *Tracker) ListChangedPaths(root string) ([]string, error) {
	changes, err := t.Diff(root)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(changes))
	for i, c := range changes {
		prefix := map[ChangeType]string{
			ChangeCreated: "+", ChangeModified: "~", ChangeDeleted: "-",
		}
		paths[i] = fmt.Sprintf("%s %s", prefix[c.Change], c.Path)
	}
	return paths, nil
}

// WatchDir polls a directory at the given interval and calls onChange when files
// are created, modified, or deleted. Returns a stop function.
func (t *Tracker) WatchDir(root string, interval time.Duration, onChange func([]FileChange)) (func(), error) {
	_, err := t.TakeSnapshot(root)
	if err != nil {
		return nil, err
	}

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				changes, err := t.Diff(root)
				if err != nil {
					continue
				}
				if len(changes) > 0 && onChange != nil {
					onChange(changes)
				}
			}
		}
	}()

	return func() { close(done) }, nil
}

// fileChecksum returns the SHA-256 hex digest of a file's contents.
func fileChecksum(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, io.LimitReader(f, 10<<20)) // 10MB limit
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Glob patterns to exclude from snapshot (dotfiles, node_modules, .git, etc.)
var DefaultExcludePatterns = []string{
	".git", "node_modules", ".venv", "__pycache__", "target", "build", "dist",
	".DS_Store", ".cache", ".mneme", "*.tmp", "*.log",
}

// ShouldExclude checks if a path matches any exclude pattern.
func ShouldExclude(path string, patterns []string) bool {
	name := filepath.Base(path)
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, name); matched {
			return true
		}
		if strings.Contains(path, "/"+p+"/") || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}
