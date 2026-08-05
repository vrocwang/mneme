package learning

import (
	"sync"

	"fmt"
	"math"
	"sort"
	"time"
)

// ── ProfileFacet (persisted row) ────────────────────────────────────────

// ProfileFacet is a single row in the user_profile_facets table.
type ProfileFacet struct {
	FacetID      string            `json:"facet_id"`
	Key          string            `json:"key"`
	Value        string            `json:"value"`
	Confidence   float64           `json:"confidence"`
	Stability    float64           `json:"stability"`
	State        FacetState        `json:"state"`
	UserState    UserState         `json:"user_state"`
	Class        string            `json:"class,omitempty"`
	EvidenceRefs []EvidenceRef     `json:"evidence_refs,omitempty"`
	CueFamilies  map[string]uint32 `json:"cue_families,omitempty"`
	LastSeenAt   float64           `json:"last_seen_at"`
	FirstSeenAt  float64           `json:"first_seen_at"`
}

// FullKey returns the class-prefixed key.
func (f *ProfileFacet) FullKey() string {
	if f.Class != "" {
		return f.Class + "/" + f.Key
	}
	return f.Key
}

// ── ComputedFacet (intermediate during rebuild) ─────────────────────────

type computedFacet struct {
	Class        FacetClass
	Key          string
	Value        string
	Confidence   float64
	Stability    float64
	State        FacetState
	UserState    UserState
	HasExplicit  bool
	EvidenceRefs []EvidenceRef
	CueFamilies  map[string]uint32
	FirstSeenAt  float64
	LastSeenAt   float64
}

// ── StabilityDetector ───────────────────────────────────────────────────

// StabilityDetector drains the candidate buffer, rebuilds facet scores using
// exponential decay, enforces per-class budgets, and persists results.
type StabilityDetector struct {
	mu     sync.Mutex
	cache  FacetCache
	buffer *CandidateBuffer
}

// FacetCache is the minimal persistence interface for the detector.
type FacetCache interface {
	ListAll() ([]ProfileFacet, error)
	Upsert(f ProfileFacet) error
	DropBelowThreshold(threshold float64) error
}

// NewStabilityDetector creates a detector backed by the given cache.
func NewStabilityDetector(cache FacetCache) *StabilityDetector {
	return &StabilityDetector{
		cache:  cache,
		buffer: GlobalBuffer(),
	}
}

// RebuildOutcome summarises a rebuild pass.
type RebuildOutcome struct {
	Added     int       `json:"added"`
	Evicted   int       `json:"evicted"`
	Kept      int       `json:"kept"`
	TotalSize int       `json:"total_size"`
	RebuiltAt time.Time `json:"rebuilt_at"`
}

// Rebuild drains the candidate buffer, recomputes all facet scores, enforces
// per-class budgets, and persists the results.
func (d *StabilityDetector) Rebuild(now time.Time) (*RebuildOutcome, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	nowSecs := float64(now.Unix())

	// 1. Drain the candidate buffer.
	candidates := d.buffer.Drain()

	// 2. Load existing facets.
	existing, err := d.cache.ListAll()
	if err != nil {
		return nil, fmt.Errorf("list existing facets: %w", err)
	}
	existingByKey := make(map[string]ProfileFacet)
	for _, f := range existing {
		existingByKey[f.FullKey()] = f
	}

	// 3. Group candidates by (class, key).
	type groupKey struct {
		class FacetClass
		key   string
	}
	groups := make(map[groupKey][]LearningCandidate)
	for _, c := range candidates {
		gk := groupKey{c.Class, c.Key}
		groups[gk] = append(groups[gk], c)
	}

	// Also add empty groups for existing facets (so they decay without new evidence).
	for _, f := range existing {
		if f.Class != "" && f.Key != "" {
			class := FacetClass(f.Class)
			gk := groupKey{class, f.Key}
			if _, ok := groups[gk]; !ok {
				groups[gk] = nil
			}
		}
	}

	// 4. Score + resolve each group.
	var computed []computedFacet
	for gk, cands := range groups {
		cf := d.resolveGroup(gk.class, gk.key, cands, existingByKey, nowSecs)
		if cf != nil {
			computed = append(computed, *cf)
		}
	}

	// 5. Budget enforcement per class.
	final := d.enforceBudgets(computed)

	// 6. Persist.
	added := 0
	evicted := 0
	kept := 0
	for _, cf := range final {
		f := ProfileFacet{
			FacetID:      fmt.Sprintf("%s/%s", cf.Class, cf.Key),
			Key:          cf.Key,
			Value:        cf.Value,
			Confidence:   cf.Confidence,
			Stability:    cf.Stability,
			State:        cf.State,
			UserState:    cf.UserState,
			Class:        string(cf.Class),
			EvidenceRefs: cf.EvidenceRefs,
			CueFamilies:  cf.CueFamilies,
			LastSeenAt:   cf.LastSeenAt,
			FirstSeenAt:  cf.FirstSeenAt,
		}
		if err := d.cache.Upsert(f); err != nil {
			return nil, fmt.Errorf("upsert facet %s: %w", f.FullKey(), err)
		}

		switch cf.State {
		case StateDropped:
			evicted++
		default:
			if _, existed := existingByKey[f.FullKey()]; existed {
				kept++
			} else {
				added++
			}
		}
	}

	// Remove dropped facets below threshold.
	if err := d.cache.DropBelowThreshold(tauEvict); err != nil {
		return nil, fmt.Errorf("drop below threshold: %w", err)
	}

	return &RebuildOutcome{
		Added:     added,
		Evicted:   evicted,
		Kept:      kept,
		TotalSize: added + kept,
		RebuiltAt: now,
	}, nil
}

// resolveGroup computes the winning value and stability for a (class, key) group.
func (d *StabilityDetector) resolveGroup(class FacetClass, key string, candidates []LearningCandidate, existingByKey map[string]ProfileFacet, nowSecs float64) *computedFacet {
	fullKey := string(class) + "/" + key

	// Check for pinned/forgotten user state.
	if existing, ok := existingByKey[fullKey]; ok {
		switch existing.UserState {
		case UserPinned:
			return &computedFacet{
				Class: class, Key: key, Value: existing.Value,
				Confidence: existing.Confidence, Stability: math.Inf(1),
				State: StateActive, UserState: UserPinned,
				EvidenceRefs: existing.EvidenceRefs, CueFamilies: existing.CueFamilies,
				FirstSeenAt: existing.FirstSeenAt, LastSeenAt: nowSecs,
			}
		case UserForgotten:
			return &computedFacet{Class: class, Key: key, State: StateDropped, UserState: UserForgotten}
		}
	}

	// No candidates and no existing row: drop.
	if len(candidates) == 0 {
		if existing, ok := existingByKey[fullKey]; ok {
			// Existing row with no new evidence — decay it.
			dt := nowSecs - existing.LastSeenAt
			if dt < 0 {
				dt = 0
			}
			halfLife := classHalfLife(class)
			decayed := existing.Stability * math.Exp(-dt/halfLife)
			state := stateFromStability(decayed, UserAuto)

			// Preserve value and evidence.
			return &computedFacet{
				Class: class, Key: key, Value: existing.Value,
				Confidence: existing.Confidence, Stability: decayed,
				State: state, UserState: UserAuto,
				EvidenceRefs: existing.EvidenceRefs, CueFamilies: existing.CueFamilies,
				FirstSeenAt: existing.FirstSeenAt, LastSeenAt: nowSecs,
			}
		}
		return nil
	}

	// Select winning value via argmax(stability).
	winningValue := selectWinningValue(class, key, candidates, existingByKey, nowSecs)

	// Compute aggregate stability.
	aggScore, hasExplicit := aggregateStability(class, candidates, existingByKey, fullKey, nowSecs)

	// Compute final stability: aggregate_score * cue_mult.
	cueMult := 1.0
	if hasExplicit {
		cueMult = 2.0
	}
	finalStability := aggScore * cueMult

	// Determine state.
	state := stateFromStability(finalStability, UserAuto)

	// Build evidence refs.
	var refs []EvidenceRef
	seen := make(map[string]bool)
	for _, c := range candidates {
		sig := fmt.Sprintf("%s:%s:%s", c.Evidence.Type, c.Evidence.SourceID, c.Evidence.ChunkID)
		if !seen[sig] {
			seen[sig] = true
			refs = append(refs, c.Evidence)
		}
	}

	// Count cue families.
	cueFamilies := make(map[string]uint32)
	for _, c := range candidates {
		cueFamilies[string(c.CueFamily)]++
	}

	firstSeen := nowSecs
	lastSeen := nowSecs
	if existing, ok := existingByKey[fullKey]; ok {
		firstSeen = existing.FirstSeenAt
		lastSeen = nowSecs
	}
	for _, c := range candidates {
		if c.ObservedAt < firstSeen {
			firstSeen = c.ObservedAt
		}
		if c.ObservedAt > lastSeen {
			lastSeen = c.ObservedAt
		}
	}

	return &computedFacet{
		Class: class, Key: key, Value: winningValue,
		Confidence: aggScore, Stability: finalStability,
		State: state, UserState: UserAuto,
		HasExplicit: hasExplicit, EvidenceRefs: refs, CueFamilies: cueFamilies,
		FirstSeenAt: firstSeen, LastSeenAt: lastSeen,
	}
}

// selectWinningValue picks the value with the highest per-value stability score.
func selectWinningValue(class FacetClass, key string, candidates []LearningCandidate, existingByKey map[string]ProfileFacet, nowSecs float64) string {
	fullKey := string(class) + "/" + key
	halfLife := classHalfLife(class)

	type valueScore struct {
		value string
		score float64
	}
	var scores []valueScore
	seen := make(map[string]bool)

	for _, c := range candidates {
		if seen[c.Value] {
			continue
		}
		seen[c.Value] = true

		score := 0.0
		for _, c2 := range candidates {
			if c2.Value == c.Value {
				dt := nowSecs - c2.ObservedAt
				if dt < 0 {
					dt = 0
				}
				score += cueWeight(c2.CueFamily) * math.Exp(-dt/halfLife) * c2.InitialConfidence
			}
		}

		// Add existing row's contribution if same value.
		if existing, ok := existingByKey[fullKey]; ok && existing.Value == c.Value {
			dt := nowSecs - existing.LastSeenAt
			if dt < 0 {
				dt = 0
			}
			score += existing.Confidence * math.Exp(-dt/halfLife)
		}

		scores = append(scores, valueScore{c.Value, score})
	}

	if len(scores) == 0 {
		if existing, ok := existingByKey[fullKey]; ok {
			return existing.Value
		}
		return ""
	}

	// Argmax.
	best := scores[0]
	for _, s := range scores[1:] {
		if s.score > best.score {
			best = s
		}
	}
	return best.value
}

// aggregateStability computes the aggregate confidence score for a group.
func aggregateStability(class FacetClass, candidates []LearningCandidate, existingByKey map[string]ProfileFacet, fullKey string, nowSecs float64) (float64, bool) {
	halfLife := classHalfLife(class)

	// Find the dominant (highest-weight) cue family among candidates.
	// This matches Rust's dominant_cue selection — only the strongest
	// signal type drives the stability score, preventing weak cues
	// from diluting strong ones.
	bestWeight := 0.0
	hasExplicit := false
	for _, c := range candidates {
		w := cueWeight(c.CueFamily)
		if w > bestWeight {
			bestWeight = w
		}
		if c.CueFamily == CueExplicit {
			hasExplicit = true
		}
	}

	// Aggregate using dominant cue only (not sum of all cues).
	// Formula: dominant_weight * exp(-dt/halfLife) * initialConfidence
	score := 0.0
	for _, c := range candidates {
		if cueWeight(c.CueFamily) < bestWeight {
			continue // skip non-dominant cues
		}
		dt := nowSecs - c.ObservedAt
		if dt < 0 {
			dt = 0
		}
		score += bestWeight * math.Exp(-dt/halfLife) * c.InitialConfidence
	}

	// Add existing row decayed.
	if existing, ok := existingByKey[fullKey]; ok {
		dt := nowSecs - existing.LastSeenAt
		if dt < 0 {
			dt = 0
		}
		score += existing.Confidence * math.Exp(-dt/halfLife)
	}

	// ln(1 + evidence_count): repeated signals compound via logarithm.
	// Matches Rust's ln(1 + count) factor that rewards repeated observations.
	evidenceCount := len(candidates)
	if existing, ok := existingByKey[fullKey]; ok {
		evidenceCount += len(existing.EvidenceRefs)
	}
	score *= math.Log1p(float64(evidenceCount))

	return score, hasExplicit
}

// stateFromStability maps (stability, userState) to FacetState.
func stateFromStability(stability float64, userState UserState) FacetState {
	if math.IsInf(stability, 1) {
		return StateActive
	}
	if stability >= tauPromote {
		return StateActive
	}
	if stability >= tauProvisional {
		return StateProvisional
	}
	if stability >= tauEvict {
		return StateCandidate
	}
	return StateDropped
}

// enforceBudgets sorts each class by stability descending and keeps the top-N.
// Excess Active rows are demoted to Provisional; a global overflow pool of 5
// Provisional rows is retained across all classes.
func (d *StabilityDetector) enforceBudgets(computed []computedFacet) []computedFacet {
	// Group by class.
	byClass := make(map[FacetClass][]computedFacet)
	for _, cf := range computed {
		byClass[cf.Class] = append(byClass[cf.Class], cf)
	}

	var final []computedFacet
	var overflow []computedFacet

	for class, facets := range byClass {
		// Sort by stability descending.
		sort.Slice(facets, func(i, j int) bool {
			return facets[i].Stability > facets[j].Stability
		})

		budget := classBudget(class)
		activeCount := 0
		for i := range facets {
			cf := &facets[i]
			if cf.State == StateActive {
				activeCount++
				if activeCount > budget {
					// Demote excess Active to Provisional for overflow pool.
					cf.State = StateProvisional
					overflow = append(overflow, *cf)
					continue
				}
			}
			final = append(final, *cf)
		}
	}

	// Overflow pool: keep top-N Provisional by stability.
	sort.Slice(overflow, func(i, j int) bool {
		return overflow[i].Stability > overflow[j].Stability
	})
	for i, cf := range overflow {
		if i < budgetOverflow {
			final = append(final, cf)
		} else if cf.State != StateDropped {
			cf.State = StateDropped
			final = append(final, cf)
		}
	}

	return final
}
