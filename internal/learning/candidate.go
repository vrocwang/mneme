package learning

import (
	"sync"
	"time"
)

// ── FacetClass ──────────────────────────────────────────────────────────

// FacetClass categorises a learned preference by its domain.
type FacetClass string

const (
	ClassStyle    FacetClass = "style"
	ClassIdentity FacetClass = "identity"
	ClassTooling  FacetClass = "tooling"
	ClassVeto     FacetClass = "veto"
	ClassGoal     FacetClass = "goal"
	ClassChannel  FacetClass = "channel"
)

// classHalfLife returns the half-life in seconds for each class.
func classHalfLife(c FacetClass) float64 {
	switch c {
	case ClassIdentity:
		return 90 * 86400
	case ClassVeto:
		return 60 * 86400
	case ClassTooling:
		return 30 * 86400
	case ClassGoal:
		return 30 * 86400
	case ClassStyle:
		return 14 * 86400
	case ClassChannel:
		return 7 * 86400
	default:
		return 14 * 86400
	}
}

// classBudget returns the max Active rows retained per class.
func classBudget(c FacetClass) int {
	switch c {
	case ClassTooling:
		return 5
	case ClassStyle:
		return 4
	case ClassIdentity:
		return 4
	case ClassVeto:
		return 3
	case ClassGoal:
		return 3
	case ClassChannel:
		return 1
	default:
		return 3
	}
}

// ── CueFamily ───────────────────────────────────────────────────────────

// CueFamily describes the evidence source type for a learning candidate.
type CueFamily string

const (
	CueExplicit   CueFamily = "explicit"
	CueStructural CueFamily = "structural"
	CueBehavioral CueFamily = "behavioral"
	CueRecurrence CueFamily = "recurrence"
)

// cueWeight returns the weight for each cue family.
func cueWeight(c CueFamily) float64 {
	switch c {
	case CueExplicit:
		return 1.0
	case CueStructural:
		return 0.9
	case CueBehavioral:
		return 0.7
	case CueRecurrence:
		return 0.6
	default:
		return 0.5
	}
}

// ── UserState ───────────────────────────────────────────────────────────

// UserState controls whether a facet is pinned or forgotten by the user.
type UserState string

const (
	UserAuto      UserState = "auto"
	UserPinned    UserState = "pinned"
	UserForgotten UserState = "forgotten"
)

// ── FacetState ──────────────────────────────────────────────────────────

// FacetState is the computed lifecycle state of a facet.
type FacetState string

const (
	StateActive      FacetState = "active"
	StateProvisional FacetState = "provisional"
	StateCandidate   FacetState = "candidate"
	StateDropped     FacetState = "dropped"
)

// ── Thresholds ──────────────────────────────────────────────────────────

const (
	tauPromote     = 1.5 // score >= 1.5 → Active
	tauProvisional = 0.7 // score >= 0.7 → Provisional
	tauEvict       = 0.4 // score >= 0.4 → Candidate; below → Dropped
)

const budgetOverflow = 5 // max Provisional rows in cross-class overflow pool

// ── EvidenceRef ─────────────────────────────────────────────────────────

// EvidenceRef identifies the source of a learning candidate.
type EvidenceRef struct {
	Type           string `json:"type"`
	EpisodicID     int64  `json:"episodic_id,omitempty"`
	SourceID       string `json:"source_id,omitempty"`
	ChunkID        string `json:"chunk_id,omitempty"`
	Toolkit        string `json:"toolkit,omitempty"`
	ConnectionID   string `json:"connection_id,omitempty"`
	Field          string `json:"field,omitempty"`
	ToolName       string `json:"tool_name,omitempty"`
	EpisodicToolID int64  `json:"episodic_tool_id,omitempty"`
	WindowLabel    string `json:"window_label,omitempty"`
}

// ── LearningCandidate ───────────────────────────────────────────────────

// LearningCandidate is a single piece of evidence about a user preference.
type LearningCandidate struct {
	Class             FacetClass  `json:"class"`
	Key               string      `json:"key"`
	Value             string      `json:"value"`
	CueFamily         CueFamily   `json:"cue_family"`
	Evidence          EvidenceRef `json:"evidence"`
	InitialConfidence float64     `json:"initial_confidence"`
	ObservedAt        float64     `json:"observed_at"` // unix seconds
}

// FullKey returns the class-prefixed key for storage.
func (c *LearningCandidate) FullKey() string {
	return string(c.Class) + "/" + c.Key
}

// ── Ring Buffer ─────────────────────────────────────────────────────────

// CandidateBuffer is a thread-safe ring buffer for learning candidates.
type CandidateBuffer struct {
	mu       sync.Mutex
	buf      []LearningCandidate
	capacity int
	head     int
	size     int
}

// NewCandidateBuffer creates a ring buffer with the given capacity.
func NewCandidateBuffer(capacity int) *CandidateBuffer {
	if capacity <= 0 {
		capacity = 1024
	}
	return &CandidateBuffer{
		buf:      make([]LearningCandidate, capacity),
		capacity: capacity,
	}
}

// Push adds a candidate to the buffer, evicting the oldest if full.
func (b *CandidateBuffer) Push(c LearningCandidate) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c.ObservedAt = float64(time.Now().Unix())
	b.buf[b.head] = c
	b.head = (b.head + 1) % b.capacity
	if b.size < b.capacity {
		b.size++
	}
}

// Drain removes and returns all candidates in FIFO order, emptying the buffer.
func (b *CandidateBuffer) Drain() []LearningCandidate {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.size == 0 {
		return nil
	}
	out := make([]LearningCandidate, b.size)
	tail := (b.head - b.size + b.capacity) % b.capacity
	for i := 0; i < b.size; i++ {
		out[i] = b.buf[(tail+i)%b.capacity]
	}
	b.size = 0
	b.head = 0
	return out
}

// Len returns the current number of candidates in the buffer.
func (b *CandidateBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.size
}

// globalCandidateBuffer is the singleton ring buffer for learning candidates.
var globalCandidateBuffer = NewCandidateBuffer(1024)

// GlobalBuffer returns the shared candidate buffer singleton.
func GlobalBuffer() *CandidateBuffer {
	return globalCandidateBuffer
}
