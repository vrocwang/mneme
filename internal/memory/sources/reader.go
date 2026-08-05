package sources

import (
	"context"
	"time"
)

// ReaderKind identifies the type of memory source reader.
type ReaderKind string

const (
	ReaderFolder       ReaderKind = "folder"
	ReaderGitHub       ReaderKind = "github"
	ReaderRSS          ReaderKind = "rss"
	ReaderWebPage      ReaderKind = "web_page"
	ReaderConversation ReaderKind = "conversation"
	ReaderComposio     ReaderKind = "composio"
)

// ReaderConfig holds configuration for a memory source reader.
type ReaderConfig struct {
	Path         string            `json:"path,omitempty"`
	URL          string            `json:"url,omitempty"`
	Recursive    bool              `json:"recursive"`
	FilePatterns []string          `json:"file_patterns,omitempty"`
	MaxFileSize  int64             `json:"max_file_size"`
	Credentials  map[string]string `json:"credentials,omitempty"`
}

// ReadItem is a single item read from a source.
type ReadItem struct {
	URI         string            `json:"uri"`
	Title       string            `json:"title,omitempty"`
	Content     string            `json:"content"`
	ContentType string            `json:"content_type"`
	Size        int64             `json:"size"`
	ModifiedAt  time.Time         `json:"modified_at,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ReadResult is the outcome of a reader operation.
type ReadResult struct {
	Items  []ReadItem `json:"items"`
	Total  int        `json:"total"`
	Cursor string     `json:"cursor,omitempty"` // for pagination
	Error  string     `json:"error,omitempty"`
}

// Reader is the interface all memory source readers implement.
// Each reader scans a specific type of source (folder, GitHub repo,
// RSS feed, web page, etc.) and produces ReadItems for ingestion.
type Reader interface {
	// Kind returns the reader kind.
	Kind() ReaderKind

	// Read scans the source and returns items.
	Read(ctx context.Context, config ReaderConfig) (*ReadResult, error)

	// ReadPage reads a page of results using a cursor from a previous read.
	ReadPage(ctx context.Context, cursor string) (*ReadResult, error)
}

// ReaderRegistry manages installed memory source readers.
type ReaderRegistry struct {
	readers map[ReaderKind]Reader
}

// NewReaderRegistry creates a reader registry.
func NewReaderRegistry() *ReaderRegistry {
	return &ReaderRegistry{readers: make(map[ReaderKind]Reader)}
}

// Register adds a reader to the registry.
func (r *ReaderRegistry) Register(reader Reader) {
	r.readers[reader.Kind()] = reader
}

// Get returns a reader by kind.
func (r *ReaderRegistry) Get(kind ReaderKind) (Reader, bool) {
	reader, ok := r.readers[kind]
	return reader, ok
}

// List returns all registered reader kinds.
func (r *ReaderRegistry) List() []ReaderKind {
	kinds := make([]ReaderKind, 0, len(r.readers))
	for k := range r.readers {
		kinds = append(kinds, k)
	}
	return kinds
}
