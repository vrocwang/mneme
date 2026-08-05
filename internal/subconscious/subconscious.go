package subconscious

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/simon/mneme/internal/agent"
)

// Engine evaluates background tasks and generates proactive behaviors.
type Engine struct {
	log        *slog.Logger
	evaluators []Evaluator
	store      *Store
	refStore   *ReflectionStore
	sqlite     *SQLiteStore // optional SQLite persistence

	mu                  sync.Mutex
	lastTickAt          time.Time
	lastActionAt        time.Time
	totalTicks          int64
	consecutiveFailures int

	// Dependencies injected for evaluator use.
	memPipeline MemoryPipeline
}

// MemoryPipeline is the interface evaluators use to query memory state.
type MemoryPipeline interface {
	Search(query string, limit int) (*MemorySearchResult, error)
	// HasExternalContent returns true when the store contains memory from
	// external sync sources created since the given time. A zero time means
	HasExternalContent(ctx context.Context, since time.Time) bool
}

// MemorySearchResult wraps memory search results for evaluators.
type MemorySearchResult struct {
	Query      string
	TotalCount int
	Items      []string // first N result summaries as text
}

// Evaluator checks if a background action should be taken.
type Evaluator interface {
	Name() string
	Evaluate(ctx context.Context, state *EngineState) ([]Action, error)
}

// EngineState provides evaluators with information about the current tick.
type EngineState struct {
	LastTickAt        time.Time
	LastActionAt      time.Time
	TotalTicks        int64
	RecentReflections []Reflection
}

// Action is a suggested proactive behavior.
type Action struct {
	Type    string                 `json:"type"` // "nudge", "suggestion", "task", "reflection", "escalation"
	Title   string                 `json:"title"`
	Message string                 `json:"message"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// Nudge generates a proactive nudge message for the user.
func (a *Action) Nudge() string {
	if a == nil {
		return ""
	}
	return a.Message
}

// ── Constructor ────────────────────────────────────────────────────

// New creates a subconscious engine with in-memory storage.
func New(log *slog.Logger) *Engine {
	return &Engine{
		log:      log,
		store:    NewStore(),
		refStore: NewReflectionStore(),
	}
}

// NewPersistent creates a subconscious engine with SQLite-backed persistence.
// Falls back to in-memory storage if the SQLite store cannot be opened.
func NewPersistent(log *slog.Logger, workspaceDir string) *Engine {
	e := &Engine{
		log:      log,
		store:    NewStore(),
		refStore: NewReflectionStore(),
	}
	sqliteStore, err := NewSQLiteStore(filepath.Join(workspaceDir, "data"))
	if err != nil {
		log.Warn("subconscious falling back to in-memory storage", "error", err)
		return e
	}
	e.sqlite = sqliteStore
	// Restore persisted state into memory stores on startup.
	if tasks, err := sqliteStore.ListTasks(); err == nil {
		for _, t := range tasks {
			e.store.AddTask(t)
		}
	}
	if refs, err := sqliteStore.ListReflections(200); err == nil {
		e.refStore.mu.Lock()
		for i := len(refs) - 1; i >= 0; i-- {
			e.refStore.reflections = append(e.refStore.reflections, refs[i])
		}
		// Parse and restore nextID
		for _, r := range refs {
			var idNum int64
			if _, err := fmt.Sscanf(r.ID, "ref-%d", &idNum); err == nil && idNum > e.refStore.nextID {
				e.refStore.nextID = idNum
			}
		}
		e.refStore.mu.Unlock()
	}
	log.Info("subconscious engine initialized with SQLite persistence",
		"tasks", len(e.store.ListTasks()),
		"reflections", e.refStore.Count(),
	)
	return e
}

// Close persists in-memory state to SQLite and closes the database.
func (e *Engine) Close() error {
	if e.sqlite == nil {
		return nil
	}
	// Persist tasks
	for _, t := range e.store.ListTasks() {
		if err := e.sqlite.UpsertTask(t); err != nil {
			e.log.Warn("failed to persist task on close", "task_id", t.ID, "error", err)
		}
	}
	// Persist reflections
	for _, r := range e.refStore.List(200) {
		if err := e.sqlite.AddReflection(r); err != nil {
			e.log.Warn("failed to persist reflection on close", "ref_id", r.ID, "error", err)
		}
	}
	return e.sqlite.Close()
}

// WithMemoryPipeline sets the memory pipeline for evaluator use.
func (e *Engine) WithMemoryPipeline(p MemoryPipeline) *Engine {
	e.memPipeline = p
	return e
}

// ── Evaluator management ───────────────────────────────────────────

// Register adds an evaluator to the engine.
func (e *Engine) Register(ev Evaluator) {
	e.evaluators = append(e.evaluators, ev)
}

// ── Tick ───────────────────────────────────────────────────────────

// Think runs all evaluators and returns suggested actions.
// Called periodically by the heartbeat. The context is scoped with a
// TurnOrigin: SubconsciousTainted when external-sync memory is present,
// or Subconscious when the store contains only internal memories.
func (e *Engine) Think(ctx context.Context) []Action {
	e.mu.Lock()
	e.totalTicks++
	state := &EngineState{
		LastTickAt:   e.lastTickAt,
		LastActionAt: e.lastActionAt,
		TotalTicks:   e.totalTicks,
	}
	e.mu.Unlock()

	// Determine origin taint level for this tick, matching Rust's
	// situation_report time-windowed taint detection. Only content synced
	// since the previous tick counts — stale external data does not taint.
	origin := agent.TurnOrigin{
		Kind:             agent.OriginTrustedAutomation,
		AutomationSource: agent.AutoSourceSubconscious,
		JobID:            fmt.Sprintf("subconscious:tick:%d", time.Now().Unix()),
	}
	cutoff := e.lastTickAt
	if e.memPipeline != nil && e.memPipeline.HasExternalContent(ctx, cutoff) {
		origin.AutomationSource = agent.AutoSourceSubconsciousTainted
	}
	ctx = agent.WithTurnOrigin(ctx, origin)

	var allActions []Action
	hadSuccess := false

	for _, ev := range e.evaluators {
		actions, err := ev.Evaluate(ctx, state)
		if err != nil {
			e.log.Warn("evaluator failed", "name", ev.Name(), "error", err)
			continue
		}
		hadSuccess = true
		for _, a := range actions {
			if a.Type == "reflection" {
				ref := Reflection{
					Kind:    a.Title,
					Body:    a.Message,
					Payload: a.Payload,
				}
				e.refStore.Add(ref)
				if e.sqlite != nil {
					e.sqlite.AddReflection(ref)
				}
			}
			// Log decision to SQLite for audit trail.
			if e.sqlite != nil {
				e.sqlite.LogDecision(e.totalTicks, ev.Name(), "action", a.Type, a.Title, a.Message)
			}
			allActions = append(allActions, a)
		}
	}

	e.mu.Lock()
	if hadSuccess {
		e.consecutiveFailures = 0
	} else {
		e.consecutiveFailures++
	}
	e.lastTickAt = time.Now()
	// Persist engine state to SQLite.
	if e.sqlite != nil {
		e.sqlite.SetEngineState("last_tick_at", e.lastTickAt.Format(time.RFC3339))
		e.sqlite.SetEngineState("total_ticks", fmt.Sprintf("%d", e.totalTicks))
		e.sqlite.SetEngineState("consecutive_failures", fmt.Sprintf("%d", e.consecutiveFailures))
	}
	e.mu.Unlock()

	return allActions
}

// NoteActivity records a user action timestamp for idle detection.
func (e *Engine) NoteActivity() {
	e.mu.Lock()
	e.lastActionAt = time.Now()
	e.mu.Unlock()
}

// ActionHandler is called for each action produced by Think(). The caller
// provides this callback at wiring time so app.go stays thin.
type ActionHandler func(action Action)

// HandleActions processes actions from Think() and dispatches to the
// provided handler. This keeps the dispatch logic in the subconscious
// package rather than in the Wails proxy layer.
func (e *Engine) HandleActions(actions []Action, handler ActionHandler) {
	for _, a := range actions {
		if handler != nil {
			handler(a)
		}
	}
}

// ShouldNudge returns true if the engine has any pending nudge-worthy actions.
// Unlike Think(), this is a read-only check that does not advance engine state.
// The caller should call Think() periodically (via heartbeat) to populate actions.
func (e *Engine) ShouldNudge() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hasPendingNudge()
}

// hasPendingNudge checks if any reflection in the store is a nudge/suggestion/escalation
// that hasn't been acted on yet. Must be called with mu held.
func (e *Engine) hasPendingNudge() bool {
	reflections := e.refStore.List(10)
	for _, r := range reflections {
		if r.ActedOnAt == nil && (r.Kind == "nudge" || r.Kind == "suggestion" || r.Kind == "escalation") {
			return true
		}
	}
	return false
}

// ── Task management ────────────────────────────────────────────────

// ScheduledTask is a recurring or one-shot task checked by evaluators.
type ScheduledTask struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Schedule  string    `json:"schedule"` // "hourly", "daily", "every_30m", "every_10m"
	Enabled   bool      `json:"enabled"`
	LastRunAt time.Time `json:"lastRunAt"`
	NextRunAt time.Time `json:"nextRunAt"`
}

// AddTask adds a scheduled task for tracking and persists to SQLite if enabled.
func (e *Engine) AddTask(task ScheduledTask) {
	e.store.AddTask(task)
	if e.sqlite != nil {
		if err := e.sqlite.UpsertTask(task); err != nil {
			e.log.Warn("failed to persist task", "task_id", task.ID, "error", err)
		}
	}
}

// ListTasks returns all tracked tasks.
func (e *Engine) ListTasks() []ScheduledTask {
	return e.store.ListTasks()
}

// GetReflections returns recent reflections.
func (e *Engine) GetReflections(limit int) []Reflection {
	return e.refStore.List(limit)
}

// GetStats returns engine statistics.
func (e *Engine) GetStats() map[string]interface{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	return map[string]interface{}{
		"total_ticks":          e.totalTicks,
		"last_tick_at":         e.lastTickAt.Format(time.RFC3339),
		"consecutive_failures": e.consecutiveFailures,
		"evaluators":           len(e.evaluators),
		"tasks":                len(e.store.ListTasks()),
		"reflections":          e.refStore.Count(),
	}
}

// ── Reflection ─────────────────────────────────────────────────────

// Reflection is a proactive observation surfaced to the user.
type Reflection struct {
	ID        string                 `json:"id"`
	Kind      string                 `json:"kind"`
	Body      string                 `json:"body"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
	CreatedAt time.Time              `json:"createdAt"`
	ActedOnAt *time.Time             `json:"actedOnAt,omitempty"`
}

// ReflectionStore persists reflections in memory with a size cap.
type ReflectionStore struct {
	mu          sync.RWMutex
	reflections []Reflection
	nextID      int64
}

func NewReflectionStore() *ReflectionStore {
	return &ReflectionStore{reflections: make([]Reflection, 0)}
}

func (r *ReflectionStore) Add(ref Reflection) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	ref.ID = fmt.Sprintf("ref-%d", r.nextID)
	ref.CreatedAt = time.Now()
	r.reflections = append([]Reflection{ref}, r.reflections...)
	// Cap at 200.
	if len(r.reflections) > 200 {
		r.reflections = r.reflections[:200]
	}
}

func (r *ReflectionStore) List(limit int) []Reflection {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > len(r.reflections) {
		limit = len(r.reflections)
	}
	result := make([]Reflection, limit)
	copy(result, r.reflections[:limit])
	return result
}

func (r *ReflectionStore) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.reflections)
}

// ── Simple in-memory task store ────────────────────────────────────

// Store holds scheduled tasks in memory.
type Store struct {
	mu    sync.RWMutex
	tasks []ScheduledTask
}

func NewStore() *Store {
	return &Store{tasks: make([]ScheduledTask, 0)}
}

func (s *Store) AddTask(task ScheduledTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tasks {
		if t.ID == task.ID {
			s.tasks[i] = task
			return
		}
	}
	s.tasks = append(s.tasks, task)
}

func (s *Store) ListTasks() []ScheduledTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]ScheduledTask, len(s.tasks))
	copy(result, s.tasks)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}
