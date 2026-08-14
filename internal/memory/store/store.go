package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// MemoryTaint classifies the provenance of a memory chunk for security gating.
// Internal memories come from agent turns; ExternalSync memories originate from
// synced sources (RSS, web pages, GitHub) and must be treated with more caution.
type MemoryTaint string

const (
	TaintInternal     MemoryTaint = "internal"
	TaintExternalSync MemoryTaint = "external_sync"
)

// MemoryChunk is a searchable piece of memory.
type MemoryChunk struct {
	ID             int64
	Source         string      // "conversation", "file", "tool", "manual"
	Taint          MemoryTaint // provenance: internal or external_sync
	Content        string
	Summary        string
	Vector         []float32
	EmbeddingModel string // model used to produce the vector, e.g. "ollama:nomic-embed-text:768"
	CreatedAt      string
}

// memoryScoredChunk pairs a chunk with a relevance score for ranking.
type memoryScoredChunk struct {
	chunk MemoryChunk
	score float64
}

// VectorResult pairs a memory chunk with its vector similarity score.
type VectorResult struct {
	Chunk      MemoryChunk
	Similarity float64
}

// Store provides full-text search over memory chunks via SQLite FTS5 with BM25 ranking.
type Store struct {
	db        *sql.DB
	encryptor *MemoryEncryptor
}

func NewStore(db *sql.DB) (*Store, error) {
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("enable WAL: %w", err)
	}

	schema := `
		CREATE TABLE IF NOT EXISTS memory_chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source TEXT NOT NULL,
			taint TEXT NOT NULL DEFAULT 'internal',
			content TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			vector BLOB,
			embedding_model TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_memory_source ON memory_chunks(source);
		-- FTS5 virtual table for BM25 full-text search over content and summary.
		-- Uses external content mode (content=) to avoid data duplication.
		CREATE VIRTUAL TABLE IF NOT EXISTS memory_chunks_fts USING fts5(
			content, summary,
			content='memory_chunks',
			content_rowid='id'
		);

		-- Triggers to keep FTS5 index in sync with memory_chunks.
		CREATE TRIGGER IF NOT EXISTS memory_chunks_ai AFTER INSERT ON memory_chunks BEGIN
			INSERT INTO memory_chunks_fts(rowid, content, summary) VALUES (new.id, new.content, new.summary);
		END;
		CREATE TRIGGER IF NOT EXISTS memory_chunks_ad AFTER DELETE ON memory_chunks BEGIN
			INSERT INTO memory_chunks_fts(memory_chunks_fts, rowid, content, summary) VALUES('delete', old.id, old.content, old.summary);
		END;
		CREATE TRIGGER IF NOT EXISTS memory_chunks_au AFTER UPDATE ON memory_chunks BEGIN
			INSERT INTO memory_chunks_fts(memory_chunks_fts, rowid, content, summary) VALUES('delete', old.id, old.content, old.summary);
			INSERT INTO memory_chunks_fts(rowid, content, summary) VALUES (new.id, new.content, new.summary);
		END;
		`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("memory store schema: %w", err)
	}

	// Migrate older databases that may lack the embedding_model column.
	var colExists bool
	rows, err := db.Query("PRAGMA table_info(memory_chunks)")
	if err == nil {
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt interface{}
			if rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk) == nil && name == "embedding_model" {
				colExists = true
				break
			}
		}
		rows.Close()
	}
	if !colExists {
		db.Exec("ALTER TABLE memory_chunks ADD COLUMN embedding_model TEXT NOT NULL DEFAULT ''")
	}

	// Migrate older databases that may lack the taint column.
	colExists = false
	rows2, err := db.Query("PRAGMA table_info(memory_chunks)")
	if err == nil {
		for rows2.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt interface{}
			if rows2.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk) == nil && name == "taint" {
				colExists = true
				break
			}
		}
		rows2.Close()
	}
	if !colExists {
		db.Exec("ALTER TABLE memory_chunks ADD COLUMN taint TEXT NOT NULL DEFAULT 'internal'")
	}

	// Populate FTS5 index from existing data if the index is empty.
	var ftsCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM memory_chunks_fts").Scan(&ftsCount); err == nil && ftsCount == 0 {
		var chunkCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM memory_chunks").Scan(&chunkCount); err == nil && chunkCount > 0 {
			db.Exec("INSERT INTO memory_chunks_fts(rowid, content, summary) SELECT id, content, summary FROM memory_chunks")
		}
	}

	return &Store{db: db}, nil
}

// EnableEncryption activates transparent AES-256-GCM encryption for memory content.
// Pass a 32-byte key; pass nil to disable. When enabled, content is encrypted
// before writing to SQLite and decrypted on all read paths.
func (s *Store) EnableEncryption(key []byte) {
	s.encryptor = NewMemoryEncryptor(key)
}

// encryptContent encrypts content if an encryptor is configured.
func (s *Store) encryptContent(plaintext string) (string, error) {
	if s.encryptor == nil {
		return plaintext, nil
	}
	return s.encryptor.EncryptContent(plaintext)
}

// decryptContent decrypts content if it was encrypted.
func (s *Store) decryptContent(stored string) (string, error) {
	if s.encryptor == nil {
		return stored, nil
	}
	return s.encryptor.DecryptContent(stored)
}

// Insert adds a memory chunk, including its vector embedding if present.
// Content and summary are sanitized for secrets and PII before storage.
func (s *Store) Insert(chunk MemoryChunk) (int64, error) {
	// Sanitize content before storage.
	sanitized, _ := SanitizeChunk(chunk)

	// Encrypt content at rest if encryption is enabled.
	var err error
	sanitized.Content, err = s.encryptContent(sanitized.Content)
	if err != nil {
		return 0, fmt.Errorf("encrypt content for insert: %w", err)
	}

	var vectorBlob interface{}
	if len(sanitized.Vector) > 0 {
		vectorBlob = EncodeVector(sanitized.Vector)
	}
	result, err := s.db.Exec(
		"INSERT INTO memory_chunks (source, taint, content, summary, vector, embedding_model) VALUES (?, ?, ?, ?, ?, ?)",
		sanitized.Source, taintStr(sanitized.Taint), sanitized.Content, sanitized.Summary, vectorBlob, sanitized.EmbeddingModel,
	)
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()
	return id, nil
}

// DeleteByContent removes chunks whose content contains the given substring.
// Returns the number of rows deleted. The FTS5 index is automatically updated
// via the DELETE trigger.
func (s *Store) DeleteByContent(substr string) (int64, error) {
	result, err := s.db.Exec(
		"DELETE FROM memory_chunks WHERE content LIKE ?",
		"%"+substr+"%",
	)
	if err != nil {
		return 0, fmt.Errorf("store delete: %w", err)
	}
	n, _ := result.RowsAffected()
	return n, nil
}

// SearchWithVector performs FTS5 BM25 search including vector columns for reranking.
// Falls back to LIKE when FTS5 is unavailable.
func (s *Store) SearchWithVector(query string, limit int) ([]MemoryChunk, error) {
	ftsQuery := escapeFTS5Query(query)
	rows, err := s.db.Query(
		`SELECT mc.id, mc.source, mc.taint, mc.content, mc.summary, mc.vector, mc.embedding_model, mc.created_at
		 FROM memory_chunks mc
		 JOIN memory_chunks_fts fts ON mc.id = fts.rowid
		 WHERE memory_chunks_fts MATCH ?
		 ORDER BY bm25(memory_chunks_fts, 0.0, 5.0, 2.0)
		 LIMIT ?`,
		ftsQuery, limit,
	)
	if err != nil {
		return s.searchWithVectorLike(query, limit)
	}
	defer rows.Close()
	return s.scanChunksWithVector(rows)
}

// searchWithVectorLike is the LIKE-based fallback when FTS5 is unavailable.
func (s *Store) searchWithVectorLike(query string, limit int) ([]MemoryChunk, error) {
	rows, err := s.db.Query(
		`SELECT id, source, taint, content, summary, vector, embedding_model, created_at
		 FROM memory_chunks
		 WHERE content LIKE ? OR summary LIKE ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		"%"+query+"%", "%"+query+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanChunksWithVector(rows)
}

// Search performs FTS5 BM25 full-text search over memory chunks.
// Falls back to LIKE search when the FTS5 table is unavailable (e.g. during migration).
func (s *Store) Search(query string, limit int) ([]MemoryChunk, error) {
	ftsQuery := escapeFTS5Query(query)
	rows, err := s.db.Query(
		`SELECT mc.id, mc.source, mc.taint, mc.content, mc.summary, mc.created_at
		 FROM memory_chunks mc
		 JOIN memory_chunks_fts fts ON mc.id = fts.rowid
		 WHERE memory_chunks_fts MATCH ?
		 ORDER BY bm25(memory_chunks_fts, 0.0, 5.0, 2.0)
		 LIMIT ?`,
		ftsQuery, limit,
	)
	if err != nil {
		// Fallback to LIKE if FTS5 is not available (schema migration pending).
		return s.searchLike(query, limit)
	}
	defer rows.Close()
	return s.scanChunks(rows)
}

// searchLike performs a simple LIKE-based search (fallback when FTS5 is unavailable).
func (s *Store) searchLike(query string, limit int) ([]MemoryChunk, error) {
	rows, err := s.db.Query(
		`SELECT id, source, taint, content, summary, created_at
		 FROM memory_chunks
		 WHERE content LIKE ? OR summary LIKE ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		"%"+query+"%", "%"+query+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanChunks(rows)
}

// ListRecent returns the most recent memory chunks.
func (s *Store) ListRecent(limit int) ([]MemoryChunk, error) {
	rows, err := s.db.Query(
		"SELECT id, source, taint, content, summary, created_at FROM memory_chunks ORDER BY created_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanChunks(rows)
}

// ListBySource returns chunks from a specific source.
func (s *Store) ListBySource(source string, limit int) ([]MemoryChunk, error) {
	rows, err := s.db.Query(
		"SELECT id, source, taint, content, summary, created_at FROM memory_chunks WHERE source = ? ORDER BY created_at DESC LIMIT ?",
		source, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanChunks(rows)
}

func (s *Store) scanChunks(rows *sql.Rows) ([]MemoryChunk, error) {
	var chunks []MemoryChunk
	for rows.Next() {
		var c MemoryChunk
		if err := rows.Scan(&c.ID, &c.Source, &c.Taint, &c.Content, &c.Summary, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Taint = normalizeTaint(string(c.Taint))
		var err error
		c.Content, err = s.decryptContent(c.Content)
		if err != nil {
			return nil, fmt.Errorf("decrypt chunk %d content: %w", c.ID, err)
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

func (s *Store) scanChunksWithVector(rows *sql.Rows) ([]MemoryChunk, error) {
	var chunks []MemoryChunk
	for rows.Next() {
		var c MemoryChunk
		var vectorBlob []byte
		if err := rows.Scan(&c.ID, &c.Source, &c.Taint, &c.Content, &c.Summary, &vectorBlob, &c.EmbeddingModel, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Taint = normalizeTaint(string(c.Taint))
		if len(vectorBlob) > 0 {
			c.Vector, _ = DecodeVector(vectorBlob)
		}
		var err error
		c.Content, err = s.decryptContent(c.Content)
		if err != nil {
			return nil, fmt.Errorf("decrypt chunk %d content: %w", c.ID, err)
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// escapeFTS5Query escapes special characters in FTS5 query strings
// and wraps each word in double quotes. Max 8 tokens.
//
// Only strips characters that break FTS5 query syntax — non-Latin
// characters (CJK, Arabic, Cyrillic, etc.) are preserved for the
// unicode61 tokenizer used by memory_chunks_fts.
func escapeFTS5Query(q string) string {
	if q == "" {
		return `""`
	}
	// Strip FTS5-syntax-breaking characters, preserving all Unicode text.
	var cleaned strings.Builder
	for _, r := range q {
		switch r {
		case '"', '*', '^', '(', ')', '{', '}':
			cleaned.WriteRune(' ')
		default:
			cleaned.WriteRune(r)
		}
	}
	words := strings.Fields(cleaned.String())
	if len(words) == 0 {
		return `""`
	}
	if len(words) > 8 {
		words = words[:8]
	}
	escaped := make([]string, len(words))
	for i, w := range words {
		w = strings.ReplaceAll(w, `"`, `""`)
		escaped[i] = `"` + w + `"`
	}
	return strings.Join(escaped, " ")
}

// ListByModel returns all chunks with a specific embedding model signature.
func (s *Store) ListByModel(ctx context.Context, model string) ([]MemoryChunk, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, source, taint, content, summary, vector, embedding_model, created_at
		 FROM memory_chunks WHERE embedding_model = ? ORDER BY created_at DESC LIMIT 1000`, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanChunksWithVector(rows)
}

// UpdateChunk updates the embedding vector for an existing chunk.
// If chunk.Content is non-empty, it is encrypted and the content column is
// updated as well.
func (s *Store) UpdateChunk(ctx context.Context, chunk MemoryChunk) error {
	if chunk.ID <= 0 {
		return fmt.Errorf("UpdateChunk: chunk.ID is required")
	}
	var vectorBlob interface{}
	if len(chunk.Vector) > 0 {
		vectorBlob = EncodeVector(chunk.Vector)
	}
	// Encrypt content if the caller provided it.
	if chunk.Content != "" {
		var err error
		chunk.Content, err = s.encryptContent(chunk.Content)
		if err != nil {
			return fmt.Errorf("encrypt content for update: %w", err)
		}
		_, err = s.db.ExecContext(ctx,
			`UPDATE memory_chunks SET vector = ?, embedding_model = ?, content = ? WHERE id = ?`,
			vectorBlob, chunk.EmbeddingModel, chunk.Content, chunk.ID)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE memory_chunks SET vector = ?, embedding_model = ? WHERE id = ?`,
		vectorBlob, chunk.EmbeddingModel, chunk.ID)
	return err
}

// CountByTaint returns the number of memory chunks with a given taint value.
func (s *Store) CountByTaint(ctx context.Context, taint MemoryTaint) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_chunks WHERE taint = ?`, string(taint)).Scan(&count)
	return count, err
}

// CountByTaintSince returns the number of chunks with a given taint created since
// the specified cutoff time. Used by the subconscious engine for time-windowed
// taint detection (matching Rust's situation_report per-tick recency check).
func (s *Store) CountByTaintSince(ctx context.Context, taint MemoryTaint, since time.Time) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_chunks WHERE taint = ? AND created_at > ?`,
		string(taint), since.UTC().Format(time.RFC3339)).Scan(&count)
	return count, err
}

// SearchByVector performs cosine similarity search over chunks whose vector
// embedding matches the given model signature.
//
// When the SQLite vec1 extension is available (it is, via internal/sqlite),
// distance is computed natively with vec1_cos_distance and ordered in SQL —
// an exact linear scan, but SIMD-accelerated and returning results in one
// query. If that errors (e.g. a mismatched stored vector dimension), it falls
// back to the brute-force cosine scan so results are never lost.
func (s *Store) SearchByVector(queryVec []float32, limit int, modelSig string) ([]VectorResult, error) {
	if len(queryVec) == 0 {
		return nil, nil
	}

	// Native vec1 path: cosine distance in SQL, ordered ascending (distance 0
	// == identical). We convert to similarity (1 - distance) on scan.
	rows, err := s.db.Query(
		`SELECT id, source, taint, content, summary, vector, embedding_model, created_at,
		        vec1_cos_distance(?, vector) AS dist
		 FROM memory_chunks
		 WHERE vector IS NOT NULL AND embedding_model = ?
		 ORDER BY dist
		 LIMIT ?`, EncodeVector(queryVec), modelSig, limit)
	if err != nil {
		return s.searchByVectorBruteForce(queryVec, limit, modelSig)
	}
	defer rows.Close()

	var results []VectorResult
	for rows.Next() {
		var c MemoryChunk
		var vectorBlob []byte
		var dist float64
		if err := rows.Scan(&c.ID, &c.Source, &c.Taint, &c.Content, &c.Summary, &vectorBlob, &c.EmbeddingModel, &c.CreatedAt, &dist); err != nil {
			return s.searchByVectorBruteForce(queryVec, limit, modelSig)
		}
		c.Taint = normalizeTaint(string(c.Taint))
		if len(vectorBlob) > 0 {
			c.Vector, _ = DecodeVector(vectorBlob)
		}
		var err error
		c.Content, err = s.decryptContent(c.Content)
		if err != nil {
			return nil, fmt.Errorf("decrypt chunk %d content: %w", c.ID, err)
		}
		results = append(results, VectorResult{Chunk: c, Similarity: vec1Similarity(dist)})
	}
	if err := rows.Err(); err != nil {
		return s.searchByVectorBruteForce(queryVec, limit, modelSig)
	}
	return results, nil
}

// searchByVectorBruteForce is the pre-vec1 fallback: it loads every chunk
// matching the model signature and scores them with Go-side cosine similarity.
func (s *Store) searchByVectorBruteForce(queryVec []float32, limit int, modelSig string) ([]VectorResult, error) {
	rows, err := s.db.Query(
		`SELECT id, source, taint, content, summary, vector, embedding_model, created_at
		 FROM memory_chunks
		 WHERE vector IS NOT NULL AND embedding_model = ?
		 ORDER BY created_at DESC LIMIT 10000`, modelSig)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chunks, err := s.scanChunksWithVector(rows)
	if err != nil {
		return nil, err
	}

	// Score by cosine similarity and keep top N.
	var scored []memoryScoredChunk
	for _, c := range chunks {
		if len(c.Vector) == 0 {
			continue
		}
		sim := cosineSimilarity(queryVec, c.Vector)
		scored = append(scored, memoryScoredChunk{chunk: c, score: sim})
	}

	// Partial sort: keep only the top `limit` by score descending.
	sortScored(scored, limit)

	result := make([]VectorResult, 0, limit)
	for i := 0; i < len(scored) && i < limit; i++ {
		result = append(result, VectorResult{Chunk: scored[i].chunk, Similarity: scored[i].score})
	}
	return result, nil
}

// vec1Similarity converts a vec1 cosine distance (0 == identical, 1 ==
// orthogonal, 2 == opposite) into a cosine similarity clamped to [0, 1],
// matching the semantics of CosineSimilarity.
func vec1Similarity(dist float64) float64 {
	sim := 1 - dist
	if sim < 0 {
		return 0
	}
	if sim > 1 {
		return 1
	}
	return sim
}

// HybridSearch combines FTS5 BM25 text search with vector similarity via RRF fusion.
// Results from both methods are merged and ranked by a weighted combination.
func (s *Store) HybridSearch(query string, queryVec []float32, limit int, modelSig string) ([]MemoryChunk, error) {
	// Text search.
	textResults, _ := s.Search(query, limit*2)

	seen := make(map[int64]bool)
	var combined []memoryScoredChunk

	for _, c := range textResults {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		combined = append(combined, memoryScoredChunk{chunk: c, score: 0.6}) // text match base score
	}

	// Vector search.
	if len(queryVec) > 0 {
		vecResults, _ := s.SearchByVector(queryVec, limit*2, modelSig)
		for _, vr := range vecResults {
			if seen[vr.Chunk.ID] {
				continue
			}
			seen[vr.Chunk.ID] = true
			combined = append(combined, memoryScoredChunk{chunk: vr.Chunk, score: 0.4 + vr.Similarity*0.6}) // vector match score
		}
	}

	sortScored(combined, limit)

	result := make([]MemoryChunk, 0, limit)
	for i := 0; i < len(combined) && i < limit; i++ {
		result = append(result, combined[i].chunk)
	}
	return result, nil
}

// cosineSimilarity delegates to the shared CosineSimilarity in vector.go
// (same package), which clamps results to [0, 1].
func cosineSimilarity(a, b []float32) float64 {
	return CosineSimilarity(a, b)
}

// sortScored partially sorts a slice of scored chunks by score descending,
// keeping only the top `limit` entries.
func sortScored(items []memoryScoredChunk, limit int) {
	if len(items) <= 1 || limit <= 0 {
		return
	}
	n := len(items)
	if limit < n {
		n = limit
	}
	// Selection sort finding the top N.
	for i := 0; i < n; i++ {
		best := i
		for j := i + 1; j < len(items); j++ {
			if items[j].score > items[best].score {
				best = j
			}
		}
		if best != i {
			items[i], items[best] = items[best], items[i]
		}
	}
}

// taintStr returns the string representation of a MemoryTaint, defaulting
// to TaintInternal for empty values. Unknown taint values map to
// TaintExternalSync for fail-closed security (matching Rust's
// MemoryTaint::from_db_str defaulting to ExternalSync).
func taintStr(t MemoryTaint) string {
	if t == "" {
		return string(TaintInternal)
	}
	return string(t)
}

// normalizeTaint maps a raw taint value from the database to a known constant.
// Unknown values default to TaintExternalSync for fail-closed security.
func normalizeTaint(raw string) MemoryTaint {
	switch MemoryTaint(raw) {
	case TaintInternal, TaintExternalSync:
		return MemoryTaint(raw)
	default:
		return TaintExternalSync
	}
}
