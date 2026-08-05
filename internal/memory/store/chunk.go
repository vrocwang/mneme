package store

import "time"

// SourceKind classifies the origin of a memory chunk for chunking strategy.
type SourceKind string

const (
	SourceKindChat     SourceKind = "chat"
	SourceKindEmail    SourceKind = "email"
	SourceKindDocument SourceKind = "document"
	SourceKindCode     SourceKind = "code"
	SourceKindWeb      SourceKind = "web"
	SourceKindFile     SourceKind = "file"
	SourceKindUnknown  SourceKind = "unknown"
)

// DataSource identifies the external system that produced this chunk.
type DataSource string

const (
	DataSourceConversation DataSource = "conversation"
	DataSourceFile         DataSource = "file"
	DataSourceTool         DataSource = "tool"
	DataSourceManual       DataSource = "manual"
	DataSourceSlack        DataSource = "slack"
	DataSourceGmail        DataSource = "gmail"
	DataSourceGitHub       DataSource = "github"
	DataSourceNotion       DataSource = "notion"
	DataSourceLinear       DataSource = "linear"
	DataSourceRSS          DataSource = "rss"
	DataSourceWebPage      DataSource = "web_page"
	DataSourceClickUp      DataSource = "clickup"
)

// Metadata carries provenance and model information for a chunk.
type Metadata struct {
	// SourceKind is the content classification for chunking strategy.
	SourceKind SourceKind `json:"source_kind,omitempty"`

	// DataSource identifies the external system.
	DataSource DataSource `json:"data_source,omitempty"`

	// EmbeddingModel is the model used to produce the vector.
	EmbeddingModel string `json:"embedding_model,omitempty"`

	// ChunkIndex is the position within a multi-chunk document (0-based).
	ChunkIndex int `json:"chunk_index,omitempty"`

	// TotalChunks is the total number of chunks for this source item.
	TotalChunks int `json:"total_chunks,omitempty"`

	// SourceURI is a stable identifier for the source (e.g. file path, URL).
	SourceURI string `json:"source_uri,omitempty"`

	// SourceTitle is a human-readable title for the source.
	SourceTitle string `json:"source_title,omitempty"`

	// ContentHash is a SHA-256 hash of the original content for dedup.
	ContentHash string `json:"content_hash,omitempty"`

	// Language is the detected language code (e.g. "en", "zh").
	Language string `json:"language,omitempty"`

	// CreatedAt is when the original content was created (may differ from
	// chunk insertion time).
	CreatedAt time.Time `json:"created_at,omitempty"`

	// Tags are user-defined or auto-extracted tags.
	Tags []string `json:"tags,omitempty"`
}

// RawRef is a back-pointer to the original content for traceability.
type RawRef struct {
	// SourceURI matches Metadata.SourceURI.
	SourceURI string `json:"source_uri"`

	// Offset is the byte offset into the original content.
	Offset int64 `json:"offset"`

	// Length is the byte length of this chunk in the original content.
	Length int64 `json:"length"`
}

// Chunk is a searchable piece of memory with full provenance metadata.
type Chunk struct {
	ID             int64       `json:"id"`
	Content        string      `json:"content"`
	Summary        string      `json:"summary,omitempty"`
	Source         string      `json:"source"`
	Taint          MemoryTaint `json:"taint"`
	Vector         []float32   `json:"vector,omitempty"`
	EmbeddingModel string      `json:"embedding_model"`
	Metadata       Metadata    `json:"metadata,omitempty"`
	RawRef         *RawRef     `json:"raw_ref,omitempty"`
	CreatedAt      string      `json:"created_at"`
}

// ChunkToMemoryChunk converts a Chunk to the legacy MemoryChunk for store
// compatibility. This bridge allows incremental migration.
func ChunkToMemoryChunk(c Chunk) MemoryChunk {
	return MemoryChunk{
		ID:             c.ID,
		Source:         c.Source,
		Taint:          c.Taint,
		Content:        c.Content,
		Summary:        c.Summary,
		Vector:         c.Vector,
		EmbeddingModel: c.EmbeddingModel,
		CreatedAt:      c.CreatedAt,
	}
}

// MemoryChunkToChunk promotes a legacy MemoryChunk to the new Chunk type.
func MemoryChunkToChunk(mc MemoryChunk) Chunk {
	return Chunk{
		ID:             mc.ID,
		Content:        mc.Content,
		Summary:        mc.Summary,
		Source:         mc.Source,
		Taint:          mc.Taint,
		Vector:         mc.Vector,
		EmbeddingModel: mc.EmbeddingModel,
		CreatedAt:      mc.CreatedAt,
		Metadata: Metadata{
			EmbeddingModel: mc.EmbeddingModel,
		},
	}
}
