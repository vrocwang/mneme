package agent_experience

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── Types ──────────────────────────────────────────────────────────

// Outcome classifies how an agent task ended.
type Outcome string

const (
	OutcomeSuccess     Outcome = "success"
	OutcomePartial     Outcome = "partial"
	OutcomeFailed      Outcome = "failed"
	OutcomeInterrupted Outcome = "interrupted"
)

// Source records where an experience came from.
type Source string

const (
	SourceToolLoop        Source = "tool_loop"
	SourceAgentReflection Source = "agent_reflection"
	SourceManual          Source = "manual"
	SourceSkillCandidate  Source = "skill_candidate"
)

// Record captures a single agent task experience for self-learning.
type Record struct {
	ID              string    `json:"id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Source          Source    `json:"source"`
	AgentID         string    `json:"agent_id,omitempty"`
	Entrypoint      string    `json:"entrypoint,omitempty"`
	TaskFingerprint string    `json:"task_fingerprint"`
	Task            string    `json:"task"`
	ToolsUsed       []string  `json:"tools_used"`
	ToolSequence    []string  `json:"tool_sequence"`
	Outcome         Outcome   `json:"outcome"`
	ErrorClass      string    `json:"error_class,omitempty"`
	Lesson          string    `json:"lesson"`
	ReuseHint       string    `json:"reuse_hint"`
	AvoidHint       string    `json:"avoid_hint,omitempty"`
	Confidence      float64   `json:"confidence"`
	Tags            []string  `json:"tags"`
	PayloadHash     string    `json:"payload_hash,omitempty"`
	Dismissed       bool      `json:"dismissed"`
	Rounds          int       `json:"rounds"`
	Duration        string    `json:"duration"`
	Error           string    `json:"error,omitempty"`
}

// Hit is a ranked retrieval result.
type Hit struct {
	Record       Record   `json:"record"`
	Score        float64  `json:"score"`
	MatchReasons []string `json:"match_reasons"`
}

// Query specifies retrieval filters and ranking criteria.
type Query struct {
	Text       string
	Tools      []string
	Tags       []string
	AgentID    string
	Entrypoint string
	MaxHits    int
}

// ── Store (SQLite-backed) ──────────────────────────────────────────

// Store persists and retrieves agent experience records via SQLite.
type Store struct {
	mu sync.RWMutex
	db *sql.DB
}

// NewStore creates a persistent experience store backed by SQLite.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Migrate creates the schema if it doesn't exist. Idempotent.
func Migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_experiences (
			id              TEXT PRIMARY KEY,
			created_at_ms   INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000),
			updated_at_ms   INTEGER NOT NULL DEFAULT (unixepoch('subsec') * 1000),
			source          TEXT NOT NULL DEFAULT 'tool_loop',
			agent_id        TEXT,
			entrypoint      TEXT,
			task_fingerprint TEXT NOT NULL DEFAULT '',
			task            TEXT NOT NULL,
			tools_used      TEXT NOT NULL DEFAULT '[]',
			tool_sequence   TEXT NOT NULL DEFAULT '[]',
			outcome         TEXT NOT NULL DEFAULT 'success',
			error_class     TEXT,
			lesson          TEXT NOT NULL DEFAULT '',
			reuse_hint      TEXT NOT NULL DEFAULT '',
			avoid_hint      TEXT,
			confidence      REAL NOT NULL DEFAULT 0.5,
			tags            TEXT NOT NULL DEFAULT '[]',
			payload_hash    TEXT,
			dismissed       INTEGER NOT NULL DEFAULT 0,
			rounds          INTEGER NOT NULL DEFAULT 0,
			duration        TEXT NOT NULL DEFAULT '',
			error_text      TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_ae_agent ON agent_experiences(agent_id);
		CREATE INDEX IF NOT EXISTS idx_ae_outcome ON agent_experiences(outcome);
		CREATE INDEX IF NOT EXISTS idx_ae_dismissed ON agent_experiences(dismissed);
	`)
	return err
}

// Put stores or updates an experience record. Auto-generates stable ID via
// SHA-256(task + tool_sequence + outcome) if not provided.
func (s *Store) Put(r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if r.ID == "" {
		r.ID = stableID(r.Task, r.ToolSequence, r.Outcome)
	}
	if r.Task == "" {
		return fmt.Errorf("task is required")
	}
	if r.Lesson == "" {
		r.Lesson = deriveLesson(r)
	}
	if r.ReuseHint == "" {
		r.ReuseHint = deriveReuseHint(r)
	}

	now := time.Now()

	// Check for existing record to preserve created_at
	var existingCreatedAt int64
	err := s.db.QueryRow("SELECT created_at_ms FROM agent_experiences WHERE id = ?", r.ID).Scan(&existingCreatedAt)
	if err == sql.ErrNoRows {
		existingCreatedAt = now.UnixMilli()
	} else if err != nil {
		return err
	}

	// Redact secrets from text fields
	r.Task = redactText(r.Task)
	r.Lesson = redactText(r.Lesson)
	r.ReuseHint = redactText(r.ReuseHint)
	if r.AvoidHint != "" {
		r.AvoidHint = redactText(r.AvoidHint)
	}

	toolsJSON, err := json.Marshal(r.ToolsUsed)
	if err != nil {
		toolsJSON = []byte("[]")
	}
	seqJSON, err := json.Marshal(r.ToolSequence)
	if err != nil {
		seqJSON = []byte("[]")
	}
	tagsJSON, err := json.Marshal(r.Tags)
	if err != nil {
		tagsJSON = []byte("[]")
	}

	if r.TaskFingerprint == "" {
		r.TaskFingerprint = stableID(r.Task, nil, OutcomeSuccess)
	}

	_, err = s.db.Exec(`
		INSERT INTO agent_experiences
			(id, created_at_ms, updated_at_ms, source, agent_id, entrypoint,
			 task_fingerprint, task, tools_used, tool_sequence, outcome,
			 error_class, lesson, reuse_hint, avoid_hint, confidence, tags,
			 payload_hash, dismissed, rounds, duration, error_text)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			updated_at_ms = excluded.updated_at_ms,
			lesson = excluded.lesson,
			reuse_hint = excluded.reuse_hint,
			avoid_hint = excluded.avoid_hint,
			confidence = excluded.confidence,
			tags = excluded.tags,
			dismissed = excluded.dismissed,
			outcome = excluded.outcome`,
		r.ID, existingCreatedAt, now.UnixMilli(), string(r.Source), nullStr(r.AgentID), nullStr(r.Entrypoint),
		r.TaskFingerprint, truncate(r.Task, 280), string(toolsJSON), string(seqJSON),
		string(r.Outcome), nullStr(r.ErrorClass),
		truncate(r.Lesson, 280), truncate(r.ReuseHint, 280), nullStr(r.AvoidHint),
		r.Confidence, string(tagsJSON), nullStr(r.PayloadHash),
		boolToInt(r.Dismissed), r.Rounds, r.Duration, nullStr(r.Error),
	)
	return err
}

// List returns recent non-dismissed records, newest first.
// limit <= 0 means no limit.
func (s *Store) List(limit int) ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		return s.queryRows(
			"SELECT id, created_at_ms, updated_at_ms, source, agent_id, entrypoint, task_fingerprint, task, tools_used, tool_sequence, outcome, error_class, lesson, reuse_hint, avoid_hint, confidence, tags, payload_hash, dismissed, rounds, duration, error_text FROM agent_experiences WHERE dismissed = 0 ORDER BY updated_at_ms DESC",
		)
	}
	return s.queryRows(
		"SELECT id, created_at_ms, updated_at_ms, source, agent_id, entrypoint, task_fingerprint, task, tools_used, tool_sequence, outcome, error_class, lesson, reuse_hint, avoid_hint, confidence, tags, payload_hash, dismissed, rounds, duration, error_text FROM agent_experiences WHERE dismissed = 0 ORDER BY updated_at_ms DESC LIMIT ?",
		limit,
	)
}

// ListByAgent returns records for a specific agent.
func (s *Store) ListByAgent(agentID string, limit int) ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		return s.queryRows(
			"SELECT id, created_at_ms, updated_at_ms, source, agent_id, entrypoint, task_fingerprint, task, tools_used, tool_sequence, outcome, error_class, lesson, reuse_hint, avoid_hint, confidence, tags, payload_hash, dismissed, rounds, duration, error_text FROM agent_experiences WHERE agent_id = ? AND dismissed = 0 ORDER BY updated_at_ms DESC",
			agentID,
		)
	}
	return s.queryRows(
		"SELECT id, created_at_ms, updated_at_ms, source, agent_id, entrypoint, task_fingerprint, task, tools_used, tool_sequence, outcome, error_class, lesson, reuse_hint, avoid_hint, confidence, tags, payload_hash, dismissed, rounds, duration, error_text FROM agent_experiences WHERE agent_id = ? AND dismissed = 0 ORDER BY updated_at_ms DESC LIMIT ?",
		agentID, limit,
	)
}

// RecentSuccesses returns the most recent successful records for an agent.
func (s *Store) RecentSuccesses(agentID string, limit int) ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		return s.queryRows(
			"SELECT id, created_at_ms, updated_at_ms, source, agent_id, entrypoint, task_fingerprint, task, tools_used, tool_sequence, outcome, error_class, lesson, reuse_hint, avoid_hint, confidence, tags, payload_hash, dismissed, rounds, duration, error_text FROM agent_experiences WHERE agent_id = ? AND outcome = 'success' AND dismissed = 0 ORDER BY updated_at_ms DESC",
			agentID,
		)
	}
	return s.queryRows(
		"SELECT id, created_at_ms, updated_at_ms, source, agent_id, entrypoint, task_fingerprint, task, tools_used, tool_sequence, outcome, error_class, lesson, reuse_hint, avoid_hint, confidence, tags, payload_hash, dismissed, rounds, duration, error_text FROM agent_experiences WHERE agent_id = ? AND outcome = 'success' AND dismissed = 0 ORDER BY updated_at_ms DESC LIMIT ?",
		agentID, limit,
	)
}

// Dismiss marks a record as dismissed without deleting it.
func (s *Store) Dismiss(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("UPDATE agent_experiences SET dismissed = 1 WHERE id = ?", id)
	return err
}

// FindSimilar performs ranked retrieval with multi-signal scoring.
// Ranking signals (weighted): tool_overlap (0.35), tag_overlap (0.25),
// query_overlap (0.20), agent_match (0.10), entrypoint_match (0.10).
func (s *Store) FindSimilar(q Query) ([]Hit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if q.MaxHits <= 0 {
		q.MaxHits = 10
	}

	rows, err := s.db.Query(
		"SELECT id, created_at_ms, updated_at_ms, source, agent_id, entrypoint, task_fingerprint, task, tools_used, tool_sequence, outcome, error_class, lesson, reuse_hint, avoid_hint, confidence, tags, payload_hash, dismissed, rounds, duration, error_text FROM agent_experiences WHERE dismissed = 0 ORDER BY updated_at_ms DESC LIMIT ?",
		200, // fetch a window and re-rank
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records, err := scanAll(rows)
	if err != nil {
		return nil, err
	}

	queryKeywords := extractKeywords(q.Text)

	var hits []Hit
	for _, r := range records {
		score, reasons := computeScore(r, q, queryKeywords)
		if score > 0 {
			// Apply confidence weighting
			score *= r.Confidence
			hits = append(hits, Hit{Record: r, Score: score, MatchReasons: reasons})
		}
	}

	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > q.MaxHits {
		hits = hits[:q.MaxHits]
	}
	return hits, nil
}

// Stats returns summary statistics.
func (s *Store) Stats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var total, successes, partials, failures, interrupted int
	row := s.db.QueryRow("SELECT COUNT(*), SUM(CASE WHEN outcome='success' THEN 1 ELSE 0 END), SUM(CASE WHEN outcome='partial' THEN 1 ELSE 0 END), SUM(CASE WHEN outcome='failed' THEN 1 ELSE 0 END), SUM(CASE WHEN outcome='interrupted' THEN 1 ELSE 0 END) FROM agent_experiences WHERE dismissed = 0")
	if err := row.Scan(&total, &successes, &partials, &failures, &interrupted); err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return map[string]interface{}{
		"total": total, "successes": successes, "partials": partials,
		"failures": failures, "interrupted": interrupted,
	}
}

// ── Auto-capture hooks ──────────────────────────────────────────────

// TurnContext holds data needed to extract experiences from a completed turn.
type TurnContext struct {
	UserMessage       string
	AssistantResponse string
	ToolCalls         []ToolCallRecord
	TurnDurationMs    int64
	SessionID         string
	AgentID           string
	Entrypoint        string
	IterationCount    int
}

// ToolCallRecord captures a single tool invocation during a turn.
type ToolCallRecord struct {
	Name          string
	Arguments     map[string]interface{}
	Success       bool
	OutputSummary string
	DurationMs    int64
}

// ExtractCandidates derives experience candidates from a completed turn.
// Returns up to 3 candidates: successful multi-tool, repeated failures, partial success.
func ExtractCandidates(ctx TurnContext) []Record {
	if len(ctx.ToolCalls) == 0 {
		return nil
	}
	var candidates []Record

	if s := successfulMultiTool(ctx); s != nil {
		candidates = append(candidates, *s)
	}
	candidates = append(candidates, repeatedFailures(ctx)...)
	if p := partialSuccess(ctx); p != nil {
		// Deduplicate: skip if we already have a Partial outcome
		alreadyPartial := false
		for _, c := range candidates {
			if c.Outcome == OutcomePartial {
				alreadyPartial = true
				break
			}
		}
		if !alreadyPartial {
			candidates = append(candidates, *p)
		}
	}
	return candidates
}

func successfulMultiTool(ctx TurnContext) *Record {
	var successes []ToolCallRecord
	for _, tc := range ctx.ToolCalls {
		if tc.Success {
			successes = append(successes, tc)
		}
	}
	if len(successes) < 2 {
		return nil
	}
	seq := toolNames(successes)
	now := time.Now()
	return &Record{
		Source:       SourceToolLoop,
		AgentID:      ctx.AgentID,
		Entrypoint:   ctx.Entrypoint,
		Task:         truncate(redactText(ctx.UserMessage), 280),
		ToolsUsed:    uniqTools(seq),
		ToolSequence: seq,
		Outcome:      OutcomeSuccess,
		Lesson:       fmt.Sprintf("For similar tasks, the successful tool sequence was %s.", strings.Join(seq, " -> ")),
		ReuseHint:    fmt.Sprintf("Reuse %s when the task resembles: %s", strings.Join(seq, " -> "), truncate(redactText(ctx.UserMessage), 120)),
		Confidence:   0.72,
		Tags:         []string{"tool-loop", "multi-tool-success"},
		Rounds:       len(ctx.ToolCalls),
		Duration:     fmt.Sprintf("%dms", ctx.TurnDurationMs),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func repeatedFailures(ctx TurnContext) []Record {
	failCounts := make(map[string][]ToolCallRecord)
	for _, tc := range ctx.ToolCalls {
		if !tc.Success {
			failCounts[tc.Name] = append(failCounts[tc.Name], tc)
		}
	}

	now := time.Now()
	var records []Record
	for tool, calls := range failCounts {
		if len(calls) < 2 {
			continue
		}
		errClass := errorClassFromSummary(calls[0].OutputSummary)
		records = append(records, Record{
			Source:       SourceToolLoop,
			AgentID:      ctx.AgentID,
			Entrypoint:   ctx.Entrypoint,
			Task:         truncate(redactText(ctx.UserMessage), 280),
			ToolsUsed:    []string{tool},
			ToolSequence: []string{tool},
			Outcome:      OutcomeFailed,
			ErrorClass:   errClass,
			Lesson:       fmt.Sprintf("%s failed %d times in one turn%s.", tool, len(calls), formatErrClass(errClass)),
			ReuseHint:    fmt.Sprintf("When %s fails repeatedly, inspect the error class before retrying.", tool),
			AvoidHint:    fmt.Sprintf("Avoid retrying %s repeatedly without changing inputs or choosing another tool.", tool),
			Confidence:   0.68,
			Tags:         []string{"tool-loop", "repeated-failure"},
			Rounds:       len(ctx.ToolCalls),
			Duration:     fmt.Sprintf("%dms", ctx.TurnDurationMs),
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	return records
}

func partialSuccess(ctx TurnContext) *Record {
	firstFail := -1
	for i, tc := range ctx.ToolCalls {
		if !tc.Success {
			firstFail = i
			break
		}
	}
	if firstFail < 0 {
		return nil
	}
	hasLaterSuccess := false
	for i := firstFail + 1; i < len(ctx.ToolCalls); i++ {
		if ctx.ToolCalls[i].Success {
			hasLaterSuccess = true
			break
		}
	}
	if !hasLaterSuccess {
		return nil
	}

	seq := toolNames(ctx.ToolCalls)
	if len(seq) < 2 {
		return nil
	}
	now := time.Now()
	return &Record{
		Source:       SourceToolLoop,
		AgentID:      ctx.AgentID,
		Entrypoint:   ctx.Entrypoint,
		Task:         truncate(redactText(ctx.UserMessage), 280),
		ToolsUsed:    uniqTools(seq),
		ToolSequence: seq,
		Outcome:      OutcomePartial,
		Lesson:       fmt.Sprintf("The task recovered after an earlier tool failure by continuing with %s.", strings.Join(seq, " -> ")),
		ReuseHint:    "If the first tool fails, switch strategy instead of repeating the same call.",
		AvoidHint:    "Repeating the same failed call without new evidence delayed progress.",
		Confidence:   0.62,
		Tags:         []string{"tool-loop", "partial-success"},
		Rounds:       len(ctx.ToolCalls),
		Duration:     fmt.Sprintf("%dms", ctx.TurnDurationMs),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// ── Ranking ─────────────────────────────────────────────────────────

func computeScore(r Record, q Query, queryKws []string) (float64, []string) {
	var score float64
	var reasons []string

	if len(queryKws) > 0 {
		taskKws := extractKeywords(r.Task)
		overlap := keywordOverlap(queryKws, taskKws)
		if overlap > 0 {
			s := float64(overlap) / float64(maxInt(len(queryKws), 1)) * 0.20
			score += s
			reasons = append(reasons, fmt.Sprintf("query_overlap(%.2f)", s))
		}
	}

	if len(q.Tools) > 0 && len(r.ToolsUsed) > 0 {
		overlap := strSliceOverlap(q.Tools, r.ToolsUsed)
		if overlap > 0 {
			s := float64(overlap) / float64(maxInt(len(q.Tools), 1)) * 0.35
			score += s
			reasons = append(reasons, fmt.Sprintf("tool_overlap(%.2f)", s))
		}
	}

	if len(q.Tags) > 0 && len(r.Tags) > 0 {
		overlap := strSliceOverlap(q.Tags, r.Tags)
		if overlap > 0 {
			s := float64(overlap) / float64(maxInt(len(q.Tags), 1)) * 0.25
			score += s
			reasons = append(reasons, fmt.Sprintf("tag_overlap(%.2f)", s))
		}
	}

	if q.AgentID != "" && r.AgentID == q.AgentID {
		score += 0.10
		reasons = append(reasons, "agent_match(0.10)")
	}

	if q.Entrypoint != "" && r.Entrypoint == q.Entrypoint {
		score += 0.10
		reasons = append(reasons, "entrypoint_match(0.10)")
	}

	return score, reasons
}

// ── Redaction ───────────────────────────────────────────────────────

var (
	bearerRe  = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	openAIKey = regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}\b`)
	secretRe  = regexp.MustCompile(`(?i)\b(token|api[_-]?key|secret|password|passwd|pass|access[_-]?token|refresh[_-]?token)\s*([:=])\s*[^\s,;]+`)
)

func redactText(input string) string {
	out := bearerRe.ReplaceAllString(input, "Bearer [redacted]")
	out = openAIKey.ReplaceAllString(out, "sk-[redacted]")
	out = secretRe.ReplaceAllStringFunc(out, func(match string) string {
		parts := secretRe.FindStringSubmatch(match)
		if len(parts) >= 3 {
			return fmt.Sprintf("%s%s [redacted]", parts[1], parts[2])
		}
		return "[redacted]"
	})
	return out
}

// ── Stable ID ──────────────────────────────────────────────────────

func stableID(task string, toolSeq []string, outcome Outcome) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(strings.ToLower(task))))
	h.Write([]byte{0})
	for _, t := range toolSeq {
		h.Write([]byte(strings.TrimSpace(strings.ToLower(t))))
		h.Write([]byte{0})
	}
	h.Write([]byte(string(outcome)))
	return fmt.Sprintf("exp_%x", h.Sum(nil)[:12])
}

// ── Prompt rendering ────────────────────────────────────────────────

// RenderForPrompt formats experience hits as a compact prompt section.
// Caps total output at maxBytes (0 = no cap).
func RenderForPrompt(hits []Hit, maxBytes int) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<agent_experience>\n")
	remaining := maxBytes
	for i, hit := range hits {
		r := hit.Record
		line := fmt.Sprintf("- [%.0f] %s → %s (%s)\n", hit.Score*100, r.Task, r.Lesson, r.Outcome)
		if maxBytes > 0 && remaining-len(line) < 0 {
			b.WriteString(fmt.Sprintf("  ... (%d more)\n", len(hits)-i))
			break
		}
		b.WriteString(line)
		if maxBytes > 0 {
			remaining -= len(line)
		}
	}
	b.WriteString("</agent_experience>\n")
	return b.String()
}

// ── Format / Display ────────────────────────────────────────────────

// FormatRecords returns a human-readable summary of experience records.
func FormatRecords(records []Record) string {
	if len(records) == 0 {
		return "No experience records found."
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Experience records (%d):\n", len(records)))
	for _, r := range records {
		icon := outcomeIcon(r.Outcome)
		b.WriteString(fmt.Sprintf("  %s [%s] %s (%s, %.0f%%)\n", icon, r.AgentID, r.Task, r.Outcome, r.Confidence*100))
		if r.Lesson != "" {
			b.WriteString(fmt.Sprintf("    → %s\n", r.Lesson))
		}
	}
	return b.String()
}

func outcomeIcon(o Outcome) string {
	switch o {
	case OutcomeSuccess:
		return "[OK]"
	case OutcomePartial:
		return "[~] "
	case OutcomeFailed:
		return "[FAIL]"
	case OutcomeInterrupted:
		return "[STOP]"
	}
	return "[?] "
}

// ── Helpers ─────────────────────────────────────────────────────────

func deriveLesson(r Record) string {
	switch r.Outcome {
	case OutcomeSuccess:
		tools := ""
		if len(r.ToolsUsed) > 0 {
			tools = fmt.Sprintf(" using %s", strings.Join(r.ToolsUsed, ", "))
		}
		return fmt.Sprintf("Task '%s' succeeded in %d rounds%s", r.Task, r.Rounds, tools)
	case OutcomeFailed:
		if r.ErrorClass != "" {
			return fmt.Sprintf("Task '%s' failed: %s", r.Task, r.ErrorClass)
		}
		return fmt.Sprintf("Task '%s' failed after %d rounds", r.Task, r.Rounds)
	case OutcomePartial:
		return fmt.Sprintf("Task '%s' partially completed in %d rounds", r.Task, r.Rounds)
	case OutcomeInterrupted:
		return fmt.Sprintf("Task '%s' was interrupted after %d rounds", r.Task, r.Rounds)
	}
	return ""
}

func deriveReuseHint(r Record) string {
	if len(r.ToolSequence) > 0 {
		return fmt.Sprintf("Try %s for similar tasks.", strings.Join(r.ToolSequence, " -> "))
	}
	return ""
}

func toolNames(calls []ToolCallRecord) []string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	return names
}

func uniqTools(seq []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range seq {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func errorClassFromSummary(summary string) string {
	if idx := strings.Index(summary, "("); idx >= 0 {
		if end := strings.Index(summary[idx:], ")"); end >= 0 {
			return strings.TrimSpace(summary[idx+1 : idx+end])
		}
	}
	return "error"
}

func formatErrClass(class string) string {
	if class == "" || class == "error" {
		return ""
	}
	return fmt.Sprintf(" with %s", class)
}

// ── Query helpers ───────────────────────────────────────────────────

func (s *Store) queryRows(query string, args ...interface{}) ([]Record, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

func scanAll(rows *sql.Rows) ([]Record, error) {
	var records []Record
	for rows.Next() {
		var r Record
		var id, source, task, outcome, lesson, reuseHint, duration, toolsJSON, seqJSON, tagsJSON string
		var agentID, entrypoint, errorClass, avoidHint, payloadHash, errorText sql.NullString
		var createdAtMs, updatedAtMs int64
		var confidence float64
		var dismissed, rounds int

		err := rows.Scan(
			&id, &createdAtMs, &updatedAtMs, &source, &agentID, &entrypoint,
			&r.TaskFingerprint, &task, &toolsJSON, &seqJSON, &outcome,
			&errorClass, &lesson, &reuseHint, &avoidHint, &confidence, &tagsJSON,
			&payloadHash, &dismissed, &rounds, &duration, &errorText,
		)
		if err != nil {
			return nil, err
		}

		r.ID = id
		r.CreatedAt = time.UnixMilli(createdAtMs)
		r.UpdatedAt = time.UnixMilli(updatedAtMs)
		r.Source = Source(source)
		r.AgentID = agentID.String
		r.Entrypoint = entrypoint.String
		r.Task = task
		r.Outcome = Outcome(outcome)
		r.ErrorClass = errorClass.String
		r.Lesson = lesson
		r.ReuseHint = reuseHint
		r.AvoidHint = avoidHint.String
		r.Confidence = confidence
		r.PayloadHash = payloadHash.String
		r.Dismissed = dismissed != 0
		r.Rounds = rounds
		r.Duration = duration
		r.Error = errorText.String
		json.Unmarshal([]byte(toolsJSON), &r.ToolsUsed)
		json.Unmarshal([]byte(seqJSON), &r.ToolSequence)
		json.Unmarshal([]byte(tagsJSON), &r.Tags)

		records = append(records, r)
	}
	return records, rows.Err()
}

// ── Text helpers ────────────────────────────────────────────────────

func extractKeywords(text string) []string {
	words := strings.Fields(strings.ToLower(text))
	var keywords []string
	seen := make(map[string]bool)
	skipWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
		"were": true, "be": true, "been": true, "to": true, "of": true, "in": true,
		"for": true, "on": true, "with": true, "at": true, "by": true, "from": true,
		"this": true, "that": true, "it": true, "and": true, "or": true, "but": true,
		"can": true, "you": true, "me": true, "my": true, "i": true,
	}
	for _, w := range words {
		w = strings.Trim(w, ",.!?;:\"')]")
		if len(w) > 2 && !skipWords[w] && !seen[w] {
			seen[w] = true
			keywords = append(keywords, w)
		}
	}
	return keywords
}

func keywordOverlap(a, b []string) int {
	setB := make(map[string]bool, len(b))
	for _, w := range b {
		setB[w] = true
	}
	common := 0
	for _, w := range a {
		if setB[w] {
			common++
		}
	}
	return common
}

func strSliceOverlap(a, b []string) int {
	setB := make(map[string]bool, len(b))
	for _, w := range b {
		setB[strings.ToLower(w)] = true
	}
	common := 0
	for _, w := range a {
		if setB[strings.ToLower(w)] {
			common++
		}
	}
	return common
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
