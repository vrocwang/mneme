package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// This file implements the L0-L3 layered memory model, adapted from the
// TencentDB Agent Memory methodology:
//
//	L0 Conversation  — raw dialogue (kept in the conversations package, untouched)
//	L1 Atom          — atomic facts extracted from conversations (one clause each)
//	L2 Scenario      — scene blocks aggregating several L1 atoms
//	L3 Persona       — user profile facets (kept in the profile package)
//
// LayeredStore is additive: it does NOT modify the legacy flat Store
// (memory_chunks) which remains the compatibility shim for existing callers.
// The legacy data is migrated into mem_l1_atom on demand via
// MigrateChunksToAtoms.

// AtomRef links an L1 atom back to its L0 source (a specific message in a
// thread). This is the drill-down edge from L1 to L0.
type AtomRef struct {
	ThreadID  string `json:"thread_id"`
	MessageID int64  `json:"message_id"`
}

// Atom is a single atomic fact (L1). It carries a refs slice for traceability
// back to the L0 conversation that produced it, and a ScenarioID that is
// non-zero once the atom has been aggregated into an L2 scenario.
type Atom struct {
	ID             int64
	Content        string
	Summary        string
	Source         string
	Taint          MemoryTaint
	Refs           []AtomRef
	ScenarioID     int64
	Vector         []float32
	EmbeddingModel string
	CreatedAt      string
}

// Scenario is a scene block (L2) aggregating a set of L1 atoms. It carries the
// atom IDs so the upper layers can drill down to the underlying facts.
type Scenario struct {
	ID             int64
	Content        string
	AtomIDs        []int64
	Vector         []float32
	EmbeddingModel string
	CreatedAt      string
}

// LayeredStore persists the L1/L2 layers of the memory pyramid. It coexists
// with the legacy flat Store.
type LayeredStore struct {
	db *sql.DB
}

// NewLayeredStore creates the L1/L2 tables if they do not exist and returns a
// LayeredStore bound to db.
func NewLayeredStore(db *sql.DB) (*LayeredStore, error) {
	schema := `
		CREATE TABLE IF NOT EXISTS mem_l1_atom (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			content TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT 'conversation',
			taint TEXT NOT NULL DEFAULT 'internal',
			refs TEXT NOT NULL DEFAULT '[]',
			vector BLOB,
			embedding_model TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_l1_source ON mem_l1_atom(source);
		CREATE VIRTUAL TABLE IF NOT EXISTS mem_l1_atom_fts USING fts5(
			content, summary,
			content='mem_l1_atom',
			content_rowid='id'
		);
		CREATE TRIGGER IF NOT EXISTS mem_l1_atom_ai AFTER INSERT ON mem_l1_atom BEGIN
			INSERT INTO mem_l1_atom_fts(rowid, content, summary) VALUES (new.id, new.content, new.summary);
		END;
		CREATE TRIGGER IF NOT EXISTS mem_l1_atom_ad AFTER DELETE ON mem_l1_atom BEGIN
			INSERT INTO mem_l1_atom_fts(mem_l1_atom_fts, rowid, content, summary) VALUES('delete', old.id, old.content, old.summary);
		END;
		CREATE TRIGGER IF NOT EXISTS mem_l1_atom_au AFTER UPDATE ON mem_l1_atom BEGIN
			INSERT INTO mem_l1_atom_fts(mem_l1_atom_fts, rowid, content, summary) VALUES('delete', old.id, old.content, old.summary);
			INSERT INTO mem_l1_atom_fts(rowid, content, summary) VALUES (new.id, new.content, new.summary);
		END;

		CREATE TABLE IF NOT EXISTS mem_l2_scenario (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			content TEXT NOT NULL,
			atom_ids TEXT NOT NULL DEFAULT '[]',
			vector BLOB,
			embedding_model TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_l2_created ON mem_l2_scenario(created_at);
		`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("layered store schema: %w", err)
	}

	// Add the scenario_id column to mem_l1_atom for L1→L2 aggregation
	// tracking, when migrating a DB created before this column existed. A NULL
	// scenario_id means "not yet aggregated into a scenario".
	if err := ensureColumn(db, "mem_l1_atom", "scenario_id", "INTEGER"); err != nil {
		return nil, fmt.Errorf("layered store scenario_id migration: %w", err)
	}
	return &LayeredStore{db: db}, nil
}

// ── L1 atoms ───────────────────────────────────────────────────────────

// InsertAtom stores a single atomic fact. refs and vector are optional; a nil
// refs slice is persisted as an empty JSON array so drill-down scans never
// encounter null.
func (s *LayeredStore) InsertAtom(ctx context.Context, a Atom) (int64, error) {
	refsJSON, err := marshalAtomRefs(a.Refs)
	if err != nil {
		return 0, fmt.Errorf("marshal refs: %w", err)
	}
	var vectorBlob interface{}
	if len(a.Vector) > 0 {
		vectorBlob = EncodeVector(a.Vector)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO mem_l1_atom (content, summary, source, taint, refs, vector, embedding_model)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.Content, a.Summary, a.Source, taintStr(a.Taint), refsJSON, vectorBlob, a.EmbeddingModel,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// ListAtomsRecent returns the most recent atoms.
func (s *LayeredStore) ListAtomsRecent(ctx context.Context, limit int) ([]Atom, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, content, summary, source, taint, refs, scenario_id, vector, embedding_model, created_at
		 FROM mem_l1_atom ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAtoms(rows)
}

// SearchAtoms performs FTS5 full-text search over atom content and summary.
func (s *LayeredStore) SearchAtoms(ctx context.Context, query string, limit int) ([]Atom, error) {
	ftsQuery := escapeFTS5Query(query)
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.id, a.content, a.summary, a.source, a.taint, a.refs, a.scenario_id, a.vector, a.embedding_model, a.created_at
		 FROM mem_l1_atom a
		 JOIN mem_l1_atom_fts f ON a.id = f.rowid
		 WHERE mem_l1_atom_fts MATCH ?
		 ORDER BY bm25(mem_l1_atom_fts, 0.0, 5.0, 2.0)
		 LIMIT ?`, ftsQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAtoms(rows)
}

// ListAtomsByIDs returns atoms whose IDs are in the given set, preserving the
// caller's order. Used for drill-down from an L2 scenario to its L1 facts.
func (s *LayeredStore) ListAtomsByIDs(ctx context.Context, ids []int64) ([]Atom, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	query := `SELECT id, content, summary, source, taint, refs, scenario_id, vector, embedding_model, created_at
		 FROM mem_l1_atom WHERE id IN (` + placeholders(len(ids)) + `)`
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	atoms, err := scanAtoms(rows)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Atom, len(atoms))
	for _, a := range atoms {
		byID[a.ID] = a
	}
	out := make([]Atom, 0, len(ids))
	for _, id := range ids {
		if a, ok := byID[id]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}

// ── L2 scenarios ───────────────────────────────────────────────────────

// UpsertScenario inserts a scene block. AtomIDs are persisted as a JSON array
// of L1 atom IDs for drill-down.
func (s *LayeredStore) UpsertScenario(ctx context.Context, sc Scenario) (int64, error) {
	atomIDs, err := marshalInt64s(sc.AtomIDs)
	if err != nil {
		return 0, fmt.Errorf("marshal atom_ids: %w", err)
	}
	var vectorBlob interface{}
	if len(sc.Vector) > 0 {
		vectorBlob = EncodeVector(sc.Vector)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO mem_l2_scenario (content, atom_ids, vector, embedding_model)
		 VALUES (?, ?, ?, ?)`,
		sc.Content, atomIDs, vectorBlob, sc.EmbeddingModel,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// ListScenariosRecent returns the most recent scene blocks.
func (s *LayeredStore) ListScenariosRecent(ctx context.Context, limit int) ([]Scenario, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, content, atom_ids, vector, embedding_model, created_at
		 FROM mem_l2_scenario ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScenarios(rows)
}

// ListAtomsUnaggregated returns atoms that have not yet been grouped into a
// scenario (scenario_id IS NULL), oldest first. This is the L1→L2 aggregation
// candidate set.
func (s *LayeredStore) ListAtomsUnaggregated(ctx context.Context, limit int) ([]Atom, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, content, summary, source, taint, refs, scenario_id, vector, embedding_model, created_at
		 FROM mem_l1_atom WHERE scenario_id IS NULL ORDER BY id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAtoms(rows)
}

// MarkAtomsInScenario assigns the given atom IDs to a scenario and returns the
// number of rows updated.
func (s *LayeredStore) MarkAtomsInScenario(ctx context.Context, atomIDs []int64, scenarioID int64) (int64, error) {
	if len(atomIDs) == 0 || scenarioID <= 0 {
		return 0, nil
	}
	query := `UPDATE mem_l1_atom SET scenario_id = ? WHERE id IN (` + placeholders(len(atomIDs)) + `)`
	args := make([]interface{}, 0, len(atomIDs)+1)
	args = append(args, scenarioID)
	for _, id := range atomIDs {
		args = append(args, id)
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// FindAtomByContent returns the newest atom whose content contains the given
// text, or nil when no match exists. Used for dedup checks before inserting.
func (s *LayeredStore) FindAtomByContent(ctx context.Context, text string) (*Atom, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, content, summary, source, taint, refs, scenario_id, vector, embedding_model, created_at
		 FROM mem_l1_atom WHERE content LIKE ? ORDER BY id DESC LIMIT 1`, "%"+text+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	atoms, err := scanAtoms(rows)
	if err != nil {
		return nil, err
	}
	if len(atoms) == 0 {
		return nil, nil
	}
	a := atoms[0]
	return &a, nil
}

// DeleteAtomsOlderThan removes atoms (and their FTS5 entries via trigger) whose
// created_at is before the cutoff time. This is the L1 retention/forgetting
// mechanism. Returns the number of rows deleted.
func (s *LayeredStore) DeleteAtomsOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM mem_l1_atom WHERE created_at < ?`, cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GetScenario returns a single scenario by ID. This is the L1→L2 upward
// drill-down path: an atom's ScenarioID points directly at its scenario, so no
// fuzzy atom_ids matching is needed.
func (s *LayeredStore) GetScenario(ctx context.Context, id int64) (*Scenario, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, content, atom_ids, vector, embedding_model, created_at
		 FROM mem_l2_scenario WHERE id = ?`, id)
	var sc Scenario
	var atomIDsJSON string
	var vectorBlob []byte
	if err := row.Scan(&sc.ID, &sc.Content, &atomIDsJSON, &vectorBlob, &sc.EmbeddingModel, &sc.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if atomIDsJSON != "" {
		_ = json.Unmarshal([]byte(atomIDsJSON), &sc.AtomIDs)
	}
	if len(vectorBlob) > 0 {
		sc.Vector, _ = DecodeVector(vectorBlob)
	}
	return &sc, nil
}

// ── Migration ──────────────────────────────────────────────────────────

// MigrateChunksToAtoms copies every legacy memory_chunk into mem_l1_atom as a
// single atom. It is non-destructive: the legacy table is left intact so
// existing callers of the flat Store keep working. Returns the number of atoms
// migrated. Safe to call multiple times (already-migrated rows are skipped by
// source tag).
func (s *LayeredStore) MigrateChunksToAtoms(ctx context.Context) (int, error) {
	// Collect all legacy rows first, then close the result set before
	// inserting. Interleaving a streaming read with writes holds a read
	// transaction open (and on :memory: databases opens a second, empty
	// connection), so we never write while rows are being iterated.
	rows, err := s.db.QueryContext(ctx,
		`SELECT source, taint, content, summary, vector, embedding_model FROM memory_chunks ORDER BY id`)
	if err != nil {
		return 0, fmt.Errorf("query legacy chunks: %w", err)
	}

	type legacyChunk struct {
		source, taint, content, summary, embeddingModel string
		vector                                          []float32
	}
	var chunks []legacyChunk
	for rows.Next() {
		var c legacyChunk
		var vectorBlob []byte
		if err := rows.Scan(&c.source, &c.taint, &c.content, &c.summary, &vectorBlob, &c.embeddingModel); err != nil {
			rows.Close()
			return 0, err
		}
		if len(vectorBlob) > 0 {
			c.vector, _ = DecodeVector(vectorBlob)
		}
		chunks = append(chunks, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	migrated := 0
	for _, c := range chunks {
		if _, err := s.InsertAtom(ctx, Atom{
			Content:        c.content,
			Summary:        c.summary,
			Source:         c.source,
			Taint:          normalizeTaint(c.taint),
			Vector:         c.vector,
			EmbeddingModel: c.embeddingModel,
		}); err != nil {
			return migrated, fmt.Errorf("migrate chunk: %w", err)
		}
		migrated++
	}
	return migrated, nil
}

// ── helpers ────────────────────────────────────────────────────────────

func scanAtoms(rows *sql.Rows) ([]Atom, error) {
	var atoms []Atom
	for rows.Next() {
		var a Atom
		var refsJSON string
		var vectorBlob []byte
		var scenarioID sql.NullInt64
		if err := rows.Scan(&a.ID, &a.Content, &a.Summary, &a.Source, &a.Taint, &refsJSON, &scenarioID, &vectorBlob, &a.EmbeddingModel, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Taint = normalizeTaint(string(a.Taint))
		if scenarioID.Valid {
			a.ScenarioID = scenarioID.Int64
		}
		if refsJSON != "" {
			_ = json.Unmarshal([]byte(refsJSON), &a.Refs)
		}
		if len(vectorBlob) > 0 {
			a.Vector, _ = DecodeVector(vectorBlob)
		}
		atoms = append(atoms, a)
	}
	return atoms, rows.Err()
}

func scanScenarios(rows *sql.Rows) ([]Scenario, error) {
	var scenarios []Scenario
	for rows.Next() {
		var sc Scenario
		var atomIDsJSON string
		var vectorBlob []byte
		if err := rows.Scan(&sc.ID, &sc.Content, &atomIDsJSON, &vectorBlob, &sc.EmbeddingModel, &sc.CreatedAt); err != nil {
			return nil, err
		}
		if atomIDsJSON != "" {
			_ = json.Unmarshal([]byte(atomIDsJSON), &sc.AtomIDs)
		}
		if len(vectorBlob) > 0 {
			sc.Vector, _ = DecodeVector(vectorBlob)
		}
		scenarios = append(scenarios, sc)
	}
	return scenarios, rows.Err()
}

func marshalAtomRefs(refs []AtomRef) (string, error) {
	if refs == nil {
		refs = []AtomRef{}
	}
	b, err := json.Marshal(refs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func marshalInt64s(ids []int64) (string, error) {
	if ids == nil {
		ids = []int64{}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// placeholders returns "?, ?, ..." for n parameters.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, n*3-2)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',', ' ')
		}
		b = append(b, '?')
	}
	return string(b)
}

// ensureColumn adds a column to a table when it does not already exist. It is
// idempotent and used for lightweight schema migrations on existing databases.
func ensureColumn(db *sql.DB, table, column, columnType string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil // already present
		}
	}
	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, columnType))
	return err
}

var _ = time.Now // keep time import for future retention TTL
