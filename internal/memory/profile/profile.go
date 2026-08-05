// Package profile implements user profile accumulation matching Rust user_profile store.
// Accumulates structured, evidence-backed user facts, preferences, skills, roles,
// and personality traits across sessions with confidence scoring and stability states.
package profile

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// FacetType categorizes a profile entry.
type FacetType string

const (
	FacetPreference  FacetType = "preference"
	FacetSkill       FacetType = "skill"
	FacetRole        FacetType = "role"
	FacetPersonality FacetType = "personality"
	FacetContext     FacetType = "context"
)

// FacetState represents lifecycle state.
type FacetState string

const (
	StateActive      FacetState = "active"
	StateProvisional FacetState = "provisional"
	StateCandidate   FacetState = "candidate"
	StateDropped     FacetState = "dropped"
)

// UserState represents user-controlled state.
type UserState string

const (
	UserAuto      UserState = "auto"
	UserPinned    UserState = "pinned"
	UserForgotten UserState = "forgotten"
)

// Stability thresholds matching Rust defaults.
const (
	TauEvict       = 0.1
	TauProvisional = 0.3
	TauPromote     = 0.6
)

// EvidenceRef points to a source segment that contributed evidence.
type EvidenceRef struct {
	SegmentID  string  `json:"segment_id"`
	Confidence float64 `json:"confidence"`
}

// ProfileFacet is one extracted fact/preference/skill about the user.
type ProfileFacet struct {
	FacetID          string            `json:"facet_id"`
	FacetType        FacetType         `json:"facet_type"`
	Key              string            `json:"key"`
	Value            string            `json:"value"`
	Confidence       float64           `json:"confidence"`
	EvidenceCount    int               `json:"evidence_count"`
	SourceSegmentIDs string            `json:"source_segment_ids"`
	FirstSeenAt      float64           `json:"first_seen_at"`
	LastSeenAt       float64           `json:"last_seen_at"`
	State            FacetState        `json:"state"`
	Stability        float64           `json:"stability"`
	UserState        UserState         `json:"user_state"`
	EvidenceRefs     []EvidenceRef     `json:"evidence_refs"`
	Class            string            `json:"class"`
	CueFamilies      map[string]uint32 `json:"cue_families"`
}

// Store persists user profile facets to SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a profile store (tables expected to exist).
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// UpsertFacet inserts or updates a profile facet. On conflict (same facet_type + key):
// increments evidence_count, updates last_seen_at, appends segment ID to source.
// Overwrites value only if new confidence >= existing confidence.
func (s *Store) UpsertFacet(facet *ProfileFacet) error {
	now := float64(time.Now().UnixNano()) / 1e9

	// Check existing.
	existing, _ := s.GetByKey(string(facet.FacetType), facet.Key)
	if existing != nil {
		// Increment evidence.
		facet.EvidenceCount = existing.EvidenceCount + 1
		facet.FirstSeenAt = existing.FirstSeenAt
		facet.State = existing.State
		facet.Stability = existing.Stability
		facet.UserState = existing.UserState

		// Append segment to source.
		if facet.SourceSegmentIDs != "" {
			if existing.SourceSegmentIDs != "" {
				facet.SourceSegmentIDs = existing.SourceSegmentIDs + "," + facet.SourceSegmentIDs
			}
		} else {
			facet.SourceSegmentIDs = existing.SourceSegmentIDs
		}

		// Only overwrite value if new confidence >= existing.
		if facet.Confidence < existing.Confidence {
			facet.Value = existing.Value
			facet.Confidence = existing.Confidence
		}

		// Merge evidence refs.
		allRefs := existing.EvidenceRefs
		allRefs = append(allRefs, facet.EvidenceRefs...)
		facet.EvidenceRefs = allRefs

		// Keep existing class if not set.
		if facet.Class == "" {
			facet.Class = existing.Class
		}
	} else {
		facet.FirstSeenAt = now
		facet.State = StateProvisional
		facet.EvidenceCount = 1
		if facet.FacetID == "" {
			facet.FacetID = "prof_" + uuid.New().String()[:12]
		}
		if facet.Class == "" {
			facet.Class = InferClassFromKey(facet.Key)
		}
	}

	facet.LastSeenAt = now
	// Recompute stability.
	facet.Stability = computeStability(facet)
	// Recompute state from stability.
	facet.State = computeState(facet)

	refsJSON, err := json.Marshal(facet.EvidenceRefs)
	if err != nil {
		refsJSON = []byte("[]")
	}
	cueJSON, err2 := json.Marshal(facet.CueFamilies)
	if err2 != nil {
		cueJSON = []byte("[]")
	}

	_, err = s.db.Exec(
		`INSERT INTO user_profile (facet_id, facet_type, key, value, confidence, evidence_count,
		 source_segment_ids, first_seen_at, last_seen_at, state, stability, user_state,
		 evidence_refs_json, class, cue_families_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(facet_type, key) DO UPDATE SET
		 value = excluded.value, confidence = excluded.confidence,
		 evidence_count = excluded.evidence_count, source_segment_ids = excluded.source_segment_ids,
		 last_seen_at = excluded.last_seen_at, state = excluded.state,
		 stability = excluded.stability, evidence_refs_json = excluded.evidence_refs_json`,
		facet.FacetID, string(facet.FacetType), facet.Key, facet.Value, facet.Confidence,
		facet.EvidenceCount, facet.SourceSegmentIDs, facet.FirstSeenAt, facet.LastSeenAt,
		string(facet.State), facet.Stability, string(facet.UserState),
		string(refsJSON), facet.Class, string(cueJSON),
	)
	return err
}

// GetByKey retrieves a facet by type and key.
func (s *Store) GetByKey(facetType, key string) (*ProfileFacet, error) {
	row := s.db.QueryRow(
		`SELECT facet_id, facet_type, key, value, confidence, evidence_count,
		 COALESCE(source_segment_ids,''), first_seen_at, last_seen_at, state, stability,
		 user_state, COALESCE(evidence_refs_json,'[]'), COALESCE(class,''),
		 COALESCE(cue_families_json,'{}')
		 FROM user_profile WHERE facet_type = ? AND key = ?`, facetType, key,
	)
	return scanFacet(row)
}

// GetActiveFacets returns active facets ordered by stability descending.
func (s *Store) GetActiveFacets(limit int) ([]ProfileFacet, error) {
	rows, err := s.db.Query(
		`SELECT facet_id, facet_type, key, value, confidence, evidence_count,
		 COALESCE(source_segment_ids,''), first_seen_at, last_seen_at, state, stability,
		 user_state, COALESCE(evidence_refs_json,'[]'), COALESCE(class,''),
		 COALESCE(cue_families_json,'{}')
		 FROM user_profile WHERE state = 'active' AND user_state != 'forgotten'
		 ORDER BY stability DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFacets(rows)
}

// GetAllFacets returns all facets.
func (s *Store) GetAllFacets() ([]ProfileFacet, error) {
	rows, err := s.db.Query(
		`SELECT facet_id, facet_type, key, value, confidence, evidence_count,
		 COALESCE(source_segment_ids,''), first_seen_at, last_seen_at, state, stability,
		 user_state, COALESCE(evidence_refs_json,'[]'), COALESCE(class,''),
		 COALESCE(cue_families_json,'{}')
		 FROM user_profile ORDER BY stability DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFacets(rows)
}

// DeleteBelowThreshold removes dropped facets below the stability threshold.
func (s *Store) DeleteBelowThreshold(threshold float64) (int64, error) {
	result, err := s.db.Exec(
		`DELETE FROM user_profile WHERE state = 'dropped' AND stability < ? AND user_state != 'pinned'`,
		threshold,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// SetUserState updates the user-controlled state for a facet.
func (s *Store) SetUserState(facetType, key string, state UserState) error {
	_, err := s.db.Exec(
		`UPDATE user_profile SET user_state = ? WHERE facet_type = ? AND key = ?`,
		string(state), facetType, key,
	)
	return err
}

// RenderProfileContext formats active facets as markdown for prompt injection.
func RenderProfileContext(facets []ProfileFacet) string {
	if len(facets) == 0 {
		return ""
	}
	groups := make(map[FacetType][]ProfileFacet)
	for _, f := range facets {
		groups[f.FacetType] = append(groups[f.FacetType], f)
	}

	var b strings.Builder
	b.WriteString("## User Profile\n\n")
	order := []FacetType{FacetRole, FacetPersonality, FacetContext, FacetPreference, FacetSkill}
	for _, ft := range order {
		fs := groups[ft]
		if len(fs) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("### %s\n", cases.Title(language.English, cases.NoLower).String(string(ft))))
		for _, f := range fs {
			b.WriteString(fmt.Sprintf("- %s: %s", f.Key, f.Value))
			if f.EvidenceCount > 1 {
				b.WriteString(fmt.Sprintf(" (×%d)", f.EvidenceCount))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// computeStability calculates a stability score from evidence count and confidence.
func computeStability(f *ProfileFacet) float64 {
	// Base: confidence weighted by log of evidence count.
	evidenceWeight := 1.0
	if f.EvidenceCount > 1 {
		evidenceWeight = 1.0 - 1.0/float64(f.EvidenceCount)
	}
	return f.Confidence * (0.3 + 0.7*evidenceWeight)
}

// computeState determines the lifecycle state from stability.
func computeState(f *ProfileFacet) FacetState {
	if f.UserState == UserPinned {
		return StateActive
	}
	if f.UserState == UserForgotten {
		return StateDropped
	}
	if f.Stability >= TauPromote {
		return StateActive
	}
	if f.Stability >= TauProvisional {
		return StateProvisional
	}
	if f.Stability >= TauEvict {
		return StateCandidate
	}
	return StateDropped
}

// InferClassFromKey derives a facet class from its key prefix.
func InferClassFromKey(key string) string {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) == 2 {
		switch parts[0] {
		case "style", "identity", "tooling", "veto", "goal", "channel":
			return parts[0]
		}
	}
	// Legacy skill: prefix.
	if strings.HasPrefix(key, "skill:") {
		return "tooling"
	}
	return "general"
}

func scanFacet(row *sql.Row) (*ProfileFacet, error) {
	f := &ProfileFacet{}
	var refsJSON, cueJSON, stateStr, userStateStr string
	err := row.Scan(
		&f.FacetID, &f.FacetType, &f.Key, &f.Value, &f.Confidence, &f.EvidenceCount,
		&f.SourceSegmentIDs, &f.FirstSeenAt, &f.LastSeenAt, &stateStr, &f.Stability,
		&userStateStr, &refsJSON, &f.Class, &cueJSON,
	)
	if err != nil {
		return nil, err
	}
	f.State = FacetState(stateStr)
	f.UserState = UserState(userStateStr)
	json.Unmarshal([]byte(refsJSON), &f.EvidenceRefs)
	json.Unmarshal([]byte(cueJSON), &f.CueFamilies)
	return f, nil
}

func scanFacets(rows *sql.Rows) ([]ProfileFacet, error) {
	var facets []ProfileFacet
	for rows.Next() {
		f := ProfileFacet{}
		var refsJSON, cueJSON, stateStr, userStateStr string
		if err := rows.Scan(
			&f.FacetID, &f.FacetType, &f.Key, &f.Value, &f.Confidence, &f.EvidenceCount,
			&f.SourceSegmentIDs, &f.FirstSeenAt, &f.LastSeenAt, &stateStr, &f.Stability,
			&userStateStr, &refsJSON, &f.Class, &cueJSON,
		); err != nil {
			return nil, err
		}
		f.State = FacetState(stateStr)
		f.UserState = UserState(userStateStr)
		json.Unmarshal([]byte(refsJSON), &f.EvidenceRefs)
		json.Unmarshal([]byte(cueJSON), &f.CueFamilies)
		facets = append(facets, f)
	}
	return facets, rows.Err()
}

// GetActiveFacets is defined above. Profile injection for the eino pipeline
// is handled by middleware.MemoryMiddleware.ModifyMessages, which reads facet
// data directly via GetActiveFacets and profile.RenderProfileContext.
