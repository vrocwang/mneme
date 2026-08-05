// Package artifacts manages generated content artifacts (images, code snippets, documents)
// created by agents during execution. Artifacts are stored in the workspace and referenced
// by thread for later retrieval.
package artifacts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Kind categorizes an artifact.
type Kind string

const (
	KindImage    Kind = "image"
	KindCode     Kind = "code"
	KindDocument Kind = "document"
	KindData     Kind = "data"
	KindOther    Kind = "other"
)

// Artifact is a generated file produced by agent execution.
type Artifact struct {
	ID        string    `json:"id"`
	ThreadID  string    `json:"thread_id"`
	Kind      Kind      `json:"kind"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	MimeType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// Store manages artifact files on disk.
type Store struct {
	baseDir string
}

// NewStore creates an artifact store rooted at workspace/artifacts.
func NewStore(workspace string) *Store {
	return &Store{baseDir: filepath.Join(workspace, "artifacts")}
}

// Save writes artifact content to disk and returns the artifact metadata.
func (s *Store) Save(threadID string, name string, kind Kind, content []byte) (*Artifact, error) {
	if err := os.MkdirAll(filepath.Join(s.baseDir, threadID), 0755); err != nil {
		return nil, fmt.Errorf("artifacts: create dir: %w", err)
	}

	id := uuid.New().String()
	relPath := filepath.Join(threadID, id+"-"+sanitizeName(name))
	fullPath := filepath.Join(s.baseDir, relPath)

	if err := os.WriteFile(fullPath, content, 0644); err != nil {
		return nil, fmt.Errorf("artifacts: write: %w", err)
	}

	return &Artifact{
		ID:        id,
		ThreadID:  threadID,
		Kind:      kind,
		Name:      name,
		Path:      relPath,
		MimeType:  detectMimeType(name, kind),
		SizeBytes: int64(len(content)),
		CreatedAt: time.Now().UTC(),
	}, nil
}

// List returns all artifacts for a thread.
func (s *Store) List(threadID string) ([]Artifact, error) {
	dir := filepath.Join(s.baseDir, threadID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var arts []Artifact
	for _, e := range entries {
		info, _ := e.Info()
		if info == nil {
			continue
		}
		arts = append(arts, Artifact{
			ID:        strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
			ThreadID:  threadID,
			Name:      e.Name(),
			Path:      filepath.Join(threadID, e.Name()),
			SizeBytes: info.Size(),
			CreatedAt: info.ModTime(),
		})
	}
	return arts, nil
}

// Read returns the content of an artifact by path.
func (s *Store) Read(threadID, name string) ([]byte, error) {
	fullPath := filepath.Join(s.baseDir, threadID, name)
	return os.ReadFile(fullPath)
}

// Delete removes an artifact.
func (s *Store) Delete(threadID, name string) error {
	fullPath := filepath.Join(s.baseDir, threadID, name)
	return os.Remove(fullPath)
}

func detectMimeType(name string, kind Kind) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case ext == ".png":
		return "image/png"
	case ext == ".jpg" || ext == ".jpeg":
		return "image/jpeg"
	case ext == ".svg":
		return "image/svg+xml"
	case ext == ".pdf":
		return "application/pdf"
	case ext == ".html":
		return "text/html"
	case ext == ".json":
		return "application/json"
	case kind == KindCode:
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

func sanitizeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, name)
	if len(name) > 200 {
		name = name[:200]
	}
	return name
}

// ── Artifact lifecycle states ────────────────────────────────────────

// Status is the lifecycle state of an artifact.
type Status string

const (
	StatusCreating Status = "creating" // artifact being written
	StatusReady    Status = "ready"    // finalized, available for reading
	StatusFailed   Status = "failed"   // creation failed
)

// artifactWithStatus wraps an Artifact with lifecycle tracking.
type artifactWithStatus struct {
	Artifact
	Status Status
}

// CreateArtifact initializes an artifact in "creating" state.
// The caller must call FinalizeArtifact or FailArtifact to transition.
func (s *Store) CreateArtifact(threadID, name string, kind Kind) (*artifactWithStatus, error) {
	if err := os.MkdirAll(filepath.Join(s.baseDir, threadID), 0755); err != nil {
		return nil, fmt.Errorf("artifacts: create dir: %w", err)
	}
	id := uuid.New().String()
	relPath := filepath.Join(threadID, id+"-"+sanitizeName(name))
	return &artifactWithStatus{
		Artifact: Artifact{
			ID:        id,
			ThreadID:  threadID,
			Kind:      kind,
			Name:      name,
			Path:      relPath,
			MimeType:  detectMimeType(name, kind),
			CreatedAt: time.Now().UTC(),
		},
		Status: StatusCreating,
	}, nil
}

// FinalizeArtifact writes content and transitions the artifact to ready.
func (s *Store) FinalizeArtifact(a *artifactWithStatus, content []byte) (*Artifact, error) {
	fullPath := filepath.Join(s.baseDir, a.Path)
	if err := os.WriteFile(fullPath, content, 0644); err != nil {
		return nil, fmt.Errorf("artifacts: finalize write: %w", err)
	}
	a.SizeBytes = int64(len(content))
	a.Status = StatusReady
	return &a.Artifact, nil
}

// FailArtifact marks an artifact as failed without writing content.
func (s *Store) FailArtifact(a *artifactWithStatus, reason string) *artifactWithStatus {
	a.Status = StatusFailed
	return a
}

// ── Tool result artifact spillover ───────────────────────────────────

const maxInlineToolResultBytes = 50 * 1024 // 50KB inline limit

// ToolResultArtifactStore spills oversized tool outputs into artifacts.
type ToolResultArtifactStore struct {
	store *Store
}

// NewToolResultArtifactStore creates a spillover store.
func NewToolResultArtifactStore(store *Store) *ToolResultArtifactStore {
	return &ToolResultArtifactStore{store: store}
}

// Spill stores oversized tool output as an artifact and returns an inline
// summary referencing the artifact. If output is under the limit, it is
// returned unchanged.
func (s *ToolResultArtifactStore) Spill(threadID, toolName string, output []byte) (inline string, artifactID string, _ error) {
	if len(output) <= maxInlineToolResultBytes {
		return string(output), "", nil
	}

	a, err := s.store.CreateArtifact(threadID, toolName+"-output", KindData)
	if err != nil {
		return "", "", err
	}
	art, err := s.store.FinalizeArtifact(a, output)
	if err != nil {
		return "", "", err
	}

	// Inline summary with truncated preview.
	preview := string(output)
	if len(preview) > 2000 {
		preview = preview[:2000] + "\n...[truncated]"
	}
	inline = fmt.Sprintf("[Tool output spilled to artifact %s — %d bytes]\n\n%s",
		art.ID, len(output), preview)
	return inline, art.ID, nil
}
