package learning

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/simon/mneme/internal/agent"
)

// sqliteStore is a SQLite-backed implementation of agent.ExperienceStore
// with multi-signal similarity search.
type sqliteStore struct {
	mu sync.RWMutex
	db *sql.DB
}

// NewSQLiteStore creates an experience store backed by SQLite.
// The caller must ensure the schema is migrated before use.
func NewSQLiteStore(db *sql.DB) (agent.ExperienceStore, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	s := &sqliteStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate experience store: %w", err)
	}
	return s, nil
}

// migrate creates the experiences table if it does not exist.
func (s *sqliteStore) migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS learning_experiences (
		id TEXT PRIMARY KEY,
		agent_id TEXT NOT NULL DEFAULT '',
		task TEXT NOT NULL DEFAULT '',
		tool_seq TEXT NOT NULL DEFAULT '',
		outcome TEXT NOT NULL DEFAULT 'success',
		lesson TEXT NOT NULL DEFAULT '',
		hints TEXT NOT NULL DEFAULT '',
		tags TEXT NOT NULL DEFAULT '',
		confidence REAL NOT NULL DEFAULT 0.5,
		dismissed INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_learning_exp_agent ON learning_experiences(agent_id);
	CREATE INDEX IF NOT EXISTS idx_learning_exp_outcome ON learning_experiences(outcome);
	CREATE INDEX IF NOT EXISTS idx_learning_exp_dismissed ON learning_experiences(dismissed);
	CREATE INDEX IF NOT EXISTS idx_learning_exp_created ON learning_experiences(created_at);
	`
	_, err := s.db.Exec(query)
	return err
}

// Search finds experiences relevant to a query using multi-signal ranking:
//   - query_overlap  (0.20) — substring match on task + lesson
//   - tool_overlap   (0.35) — tool name intersection
//   - tag_overlap    (0.25) — tag intersection
//   - agent_match    (0.10) — agent_id exact match
//   - confidence     (0.10) — raw confidence score
func (s *sqliteStore) Search(ctx context.Context, query string, limit int) ([]agent.Experience, error) {
	if limit <= 0 {
		limit = 10
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, task, tool_seq, outcome, lesson, hints, tags, confidence
		 FROM learning_experiences WHERE dismissed = 0
		 ORDER BY created_at DESC LIMIT ?`, limit*3) // overfetch for ranking
	if err != nil {
		return nil, fmt.Errorf("search experiences: %w", err)
	}
	defer rows.Close()

	type scored struct {
		exp   agent.Experience
		score float64
	}

	var candidates []scored
	queryLower := strings.ToLower(query)
	queryTokens := strings.Fields(queryLower)

	for rows.Next() {
		var exp agent.Experience
		var agentID, task, toolSeq, outcome, lesson, hints, tags string
		var confidence float64

		if err := rows.Scan(&exp.ID, &agentID, &task, &toolSeq, &outcome,
			&lesson, &hints, &tags, &confidence); err != nil {
			slog.Warn("learning: failed to scan experience row", "error", err)
			continue
		}

		exp.ThreadID = agentID
		exp.Learning = lesson
		exp.Message = task
		exp.Context = hints
		exp.Score = confidence

		// Multi-signal scoring.
		score := 0.0

		// query_overlap (0.20)
		taskLower := strings.ToLower(task)
		lessonLower := strings.ToLower(lesson)
		for _, tok := range queryTokens {
			if strings.Contains(taskLower, tok) || strings.Contains(lessonLower, tok) {
				score += 0.20 / float64(len(queryTokens))
			}
		}

		// tool_overlap (0.35) — check if any query tokens are tool names
		if toolSeq != "" {
			tools := strings.Split(toolSeq, ",")
			for _, t := range tools {
				t = strings.TrimSpace(t)
				for _, tok := range queryTokens {
					if tok == t {
						score += 0.35
						break
					}
				}
			}
		}

		// tag_overlap (0.25)
		if tags != "" {
			tagList := strings.Split(tags, ",")
			for _, tag := range tagList {
				tag = strings.TrimSpace(strings.ToLower(tag))
				for _, tok := range queryTokens {
					if strings.HasPrefix(tag, tok) || tok == tag {
						score += 0.25
						break
					}
				}
			}
		}

		// agent_match (0.10) — exact agent_id match bonus
		if agentID != "" && strings.Contains(queryLower, strings.ToLower(agentID)) {
			score += 0.10
		}

		// confidence (0.10)
		score += confidence * 0.10

		exp.Score = score
		if score > 0 {
			candidates = append(candidates, scored{exp, score})
		}
	}

	// Sort by score descending.
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	result := make([]agent.Experience, len(candidates))
	for i, c := range candidates {
		result[i] = c.exp
	}
	return result, nil
}

// Save persists an experience with richer metadata derived from the learning content.
func (s *sqliteStore) Save(ctx context.Context, exp agent.Experience) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if exp.ID == "" {
		exp.ID = fmt.Sprintf("exp_%d", time.Now().UnixNano())
	}
	now := time.Now().UTC().Format(time.RFC3339)

	// Derive richer fields from the basic experience.
	task := truncate(exp.Message, 500)
	lesson := truncate(exp.Learning, 500)
	hints := truncate(extractHints(exp.Context), 500)
	tags := extractTags(task, lesson)

	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO learning_experiences
		 (id, agent_id, task, tool_seq, outcome, lesson, hints, tags, confidence, dismissed, created_at, updated_at)
		 VALUES (?, ?, ?, '', 'success', ?, ?, ?, ?, 0, ?, ?)`,
		exp.ID, exp.ThreadID, task, lesson, hints, tags, exp.Score, now, now)
	if err != nil {
		return fmt.Errorf("save experience: %w", err)
	}
	return nil
}

// Dismiss marks an experience as dismissed so it won't appear in searches.
func (s *sqliteStore) Dismiss(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx,
		`UPDATE learning_experiences SET dismissed = 1, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// Stats returns aggregate statistics about stored experiences.
func (s *sqliteStore) Stats(ctx context.Context) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]interface{})

	var total, dismissed int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), SUM(CASE WHEN dismissed = 1 THEN 1 ELSE 0 END)
		 FROM learning_experiences`).Scan(&total, &dismissed); err != nil {
		return nil, err
	}
	stats["total"] = total
	stats["dismissed"] = dismissed
	stats["active"] = total - dismissed

	rows, err := s.db.QueryContext(ctx,
		`SELECT outcome, COUNT(*) FROM learning_experiences GROUP BY outcome`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var outcome string
			var count int
			if err := rows.Scan(&outcome, &count); err == nil {
				stats["outcome_"+outcome] = count
			}
		}
	}

	return stats, nil
}

// RecentSuccesses returns recent successful experiences.
func (s *sqliteStore) RecentSuccesses(ctx context.Context, limit int) ([]agent.Experience, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, task, outcome, lesson, hints, confidence
		 FROM learning_experiences WHERE dismissed = 0 AND outcome = 'success'
		 ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var exps []agent.Experience
	for rows.Next() {
		var exp agent.Experience
		var task, outcome, lesson, hints string
		var conf float64
		if err := rows.Scan(&exp.ID, &exp.ThreadID, &task, &outcome, &lesson, &hints, &conf); err != nil {
			slog.Warn("learning: failed to scan recent experience row", "error", err)
			continue
		}
		exp.Message = task
		exp.Learning = lesson
		exp.Context = hints
		exp.Score = conf
		exps = append(exps, exp)
	}
	return exps, nil
}

// ── helper functions ──────────────────────────────────────────────────────

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func extractHints(context string) string {
	if len(context) > 200 {
		return context[:200]
	}
	return context
}

// extractTags extracts meaningful tokens from task + lesson for tagging.
func extractTags(task, lesson string) string {
	combined := strings.ToLower(task + " " + lesson)
	words := strings.Fields(combined)
	stopwords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
		"were": true, "be": true, "been": true, "being": true, "have": true,
		"has": true, "had": true, "do": true, "does": true, "did": true,
		"will": true, "would": true, "could": true, "should": true, "may": true,
		"might": true, "can": true, "shall": true, "to": true, "of": true,
		"in": true, "for": true, "on": true, "with": true, "at": true, "by": true,
		"from": true, "as": true, "into": true, "through": true, "about": true,
		"this": true, "that": true, "it": true, "its": true, "i": true,
		"and": true, "or": true, "not": true, "but": true, "if": true,
	}

	seen := make(map[string]bool)
	var tags []string
	for _, w := range words {
		if len(w) > 3 && !stopwords[w] && !seen[w] {
			seen[w] = true
			tags = append(tags, w)
		}
		if len(tags) >= 8 {
			break
		}
	}
	return strings.Join(tags, ",")
}
