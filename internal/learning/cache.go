package learning

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// SQLiteCache persists ProfileFacets to a SQLite table.
type SQLiteCache struct {
	mu sync.Mutex
	db *sql.DB
}

// NewSQLiteCache creates a cache backed by the given SQLite connection.
// The caller must ensure the user_profile_facets table exists (created by migration).
func NewSQLiteCache(db *sql.DB) (*SQLiteCache, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	c := &SQLiteCache{db: db}
	if err := c.migrate(); err != nil {
		return nil, fmt.Errorf("migrate facet cache: %w", err)
	}
	return c, nil
}

func (c *SQLiteCache) migrate() error {
	_, err := c.db.Exec(`
		CREATE TABLE IF NOT EXISTS user_profile_facets (
			facet_id TEXT PRIMARY KEY,
			key TEXT NOT NULL,
			value TEXT NOT NULL DEFAULT '',
			confidence REAL NOT NULL DEFAULT 0,
			stability REAL NOT NULL DEFAULT 0,
			state TEXT NOT NULL DEFAULT 'candidate',
			user_state TEXT NOT NULL DEFAULT 'auto',
			class TEXT NOT NULL DEFAULT '',
			evidence_refs TEXT NOT NULL DEFAULT '[]',
			cue_families TEXT NOT NULL DEFAULT '{}',
			first_seen_at REAL NOT NULL DEFAULT 0,
			last_seen_at REAL NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_facets_class_state ON user_profile_facets(class, state);
	`)
	return err
}

// ListAll returns all facets regardless of state.
func (c *SQLiteCache) ListAll() ([]ProfileFacet, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	rows, err := c.db.Query(`SELECT facet_id, key, value, confidence, stability, state, user_state, class, evidence_refs, cue_families, first_seen_at, last_seen_at FROM user_profile_facets`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	facets := make([]ProfileFacet, 0)
	for rows.Next() {
		var f ProfileFacet
		var refsJSON, cuesJSON string
		if err := rows.Scan(&f.FacetID, &f.Key, &f.Value, &f.Confidence, &f.Stability, &f.State, &f.UserState, &f.Class, &refsJSON, &cuesJSON, &f.FirstSeenAt, &f.LastSeenAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(refsJSON), &f.EvidenceRefs); err != nil {
			f.EvidenceRefs = nil
		}
		if err := json.Unmarshal([]byte(cuesJSON), &f.CueFamilies); err != nil {
			f.CueFamilies = nil
		}
		facets = append(facets, f)
	}
	return facets, rows.Err()
}

// ListByClass returns facets matching the given class prefix.
func (c *SQLiteCache) ListByClass(class string) ([]ProfileFacet, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	rows, err := c.db.Query(`SELECT facet_id, key, value, confidence, stability, state, user_state, class, evidence_refs, cue_families, first_seen_at, last_seen_at FROM user_profile_facets WHERE class = ?`, class)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	facets := make([]ProfileFacet, 0)
	for rows.Next() {
		var f ProfileFacet
		var refsJSON, cuesJSON string
		if err := rows.Scan(&f.FacetID, &f.Key, &f.Value, &f.Confidence, &f.Stability, &f.State, &f.UserState, &f.Class, &refsJSON, &cuesJSON, &f.FirstSeenAt, &f.LastSeenAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(refsJSON), &f.EvidenceRefs); err != nil {
			f.EvidenceRefs = nil
		}
		if err := json.Unmarshal([]byte(cuesJSON), &f.CueFamilies); err != nil {
			f.CueFamilies = nil
		}
		facets = append(facets, f)
	}
	return facets, rows.Err()
}

// Upsert inserts or updates a facet.
func (c *SQLiteCache) Upsert(f ProfileFacet) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	refsJSON, err := json.Marshal(f.EvidenceRefs)
	if err != nil {
		return fmt.Errorf("marshal evidence_refs: %w", err)
	}
	var cuesJSON []byte
	cuesJSON, err = json.Marshal(f.CueFamilies)
	if err != nil {
		return fmt.Errorf("marshal cue_families: %w", err)
	}

	if f.FacetID == "" {
		f.FacetID = f.FullKey()
	}
	if f.FirstSeenAt == 0 {
		f.FirstSeenAt = f.LastSeenAt
	}

	// If state is Dropped and stability below threshold, just delete.
	if f.State == StateDropped && f.Stability < tauEvict && f.UserState != UserPinned {
		_, err = c.db.Exec(`DELETE FROM user_profile_facets WHERE facet_id = ?`, f.FacetID)
		return err
	}

	_, err = c.db.Exec(`
		INSERT INTO user_profile_facets (facet_id, key, value, confidence, stability, state, user_state, class, evidence_refs, cue_families, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(facet_id) DO UPDATE SET
			value = excluded.value,
			confidence = excluded.confidence,
			stability = excluded.stability,
			state = excluded.state,
			user_state = excluded.user_state,
			evidence_refs = excluded.evidence_refs,
			cue_families = excluded.cue_families,
			last_seen_at = excluded.last_seen_at`,
		f.FacetID, f.Key, f.Value, f.Confidence, f.Stability, string(f.State), string(f.UserState), f.Class, string(refsJSON), string(cuesJSON), f.FirstSeenAt, f.LastSeenAt)
	return err
}

// DropBelowThreshold deletes facets with Dropped state below the threshold.
func (c *SQLiteCache) DropBelowThreshold(threshold float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := c.db.Exec(`DELETE FROM user_profile_facets WHERE state = 'dropped' AND stability < ? AND user_state != 'pinned'`, threshold)
	return err
}

// SetUserState overrides the user_state of a facet.
func (c *SQLiteCache) SetUserState(facetID string, state UserState) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := c.db.Exec(`UPDATE user_profile_facets SET user_state = ? WHERE facet_id = ?`, string(state), facetID)
	return err
}

// parseFacetClass converts a string to a FacetClass, returning false if unknown.
func parseFacetClass(s string) (FacetClass, bool) {
	switch strings.ToLower(s) {
	case "style":
		return ClassStyle, true
	case "identity":
		return ClassIdentity, true
	case "tooling":
		return ClassTooling, true
	case "veto":
		return ClassVeto, true
	case "goal":
		return ClassGoal, true
	case "channel":
		return ClassChannel, true
	default:
		return "", false
	}
}

// parseCueFamily converts a string to a CueFamily, defaulting to Behavioral.
func parseCueFamily(s string) CueFamily {
	switch strings.ToLower(s) {
	case "explicit":
		return CueExplicit
	case "structural":
		return CueStructural
	case "behavioral":
		return CueBehavioral
	case "recurrence":
		return CueRecurrence
	default:
		return CueBehavioral
	}
}
