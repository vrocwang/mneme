package memory

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RulePriority indicates how critical a tool rule is.
type RulePriority string

const (
	RuleCritical RulePriority = "critical"
	RuleHigh     RulePriority = "high"
	RuleNormal   RulePriority = "normal"
	RuleLow      RulePriority = "low"
)

// RuleSource indicates where a tool rule came from.
type RuleSource string

const (
	RuleSourceManual       RuleSource = "manual"
	RuleSourceHeuristic    RuleSource = "heuristic"
	RuleSourceProgrammatic RuleSource = "programmatic"
)

// ToolRule is a durable memory rule scoped to a specific tool.
type ToolRule struct {
	ID        string       `json:"id"`
	ToolName  string       `json:"tool_name"`
	Content   string       `json:"content"`
	Priority  RulePriority `json:"priority"`
	Source    RuleSource   `json:"source"`
	Tags      []string     `json:"tags,omitempty"`
	CreatedAt string       `json:"created_at,omitempty"`
	UpdatedAt string       `json:"updated_at,omitempty"`
}

// ToolRuleStore persists tool-scoped memory rules.
type ToolRuleStore struct {
	db *sql.DB
}

func NewToolRuleStore(db *sql.DB) (*ToolRuleStore, error) {
	if err := initToolRuleTables(db); err != nil {
		return nil, fmt.Errorf("init tool_rule tables: %w", err)
	}
	return &ToolRuleStore{db: db}, nil
}

func initToolRuleTables(db *sql.DB) error {
	query := `CREATE TABLE IF NOT EXISTS tool_rules (
		id TEXT PRIMARY KEY,
		tool_name TEXT NOT NULL,
		content TEXT NOT NULL,
		priority TEXT NOT NULL DEFAULT 'normal',
		source TEXT NOT NULL DEFAULT 'manual',
		tags_json TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`
	_, err := db.Exec(query)
	if err != nil {
		return err
	}
	// Index for fast lookup by tool + priority
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_tool_rules_tool_priority ON tool_rules(tool_name, priority)`)
	return err
}

// Put upserts a tool rule.
func (s *ToolRuleStore) Put(rule ToolRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if rule.ID == "" {
		rule.ID = fmt.Sprintf("rule_%s_%s", rule.ToolName, randomSuffix(8))
	}
	if rule.Priority == "" {
		rule.Priority = RuleNormal
	}
	if rule.Source == "" {
		rule.Source = RuleSourceManual
	}

	tagsJSON := "[]"
	if len(rule.Tags) > 0 {
		b, err := json.Marshal(rule.Tags)
		if err != nil {
			b = []byte("[]")
		}
		tagsJSON = string(b)
	}

	_, err := s.db.Exec(
		`INSERT INTO tool_rules (id, tool_name, content, priority, source, tags_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			content = excluded.content,
			priority = excluded.priority,
			source = excluded.source,
			tags_json = excluded.tags_json,
			updated_at = excluded.updated_at`,
		rule.ID, rule.ToolName, rule.Content, string(rule.Priority), string(rule.Source),
		tagsJSON, now, now,
	)
	return err
}

// Get retrieves a specific tool rule by ID.
func (s *ToolRuleStore) Get(id string) (*ToolRule, error) {
	var r ToolRule
	var tagsJSON string
	err := s.db.QueryRow(
		`SELECT id, tool_name, content, priority, source, tags_json, created_at, updated_at
		 FROM tool_rules WHERE id = ?`, id,
	).Scan(&r.ID, &r.ToolName, &r.Content, &r.Priority, &r.Source, &tagsJSON, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(tagsJSON), &r.Tags)
	r.Priority = RulePriority(r.Priority)
	r.Source = RuleSource(r.Source)
	return &r, nil
}

// List returns all rules for a tool.
func (s *ToolRuleStore) List(toolName string) ([]ToolRule, error) {
	rows, err := s.db.Query(
		`SELECT id, tool_name, content, priority, source, tags_json, created_at, updated_at
		 FROM tool_rules WHERE tool_name = ? ORDER BY priority, created_at`, toolName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanToolRules(rows)
}

// Delete removes a tool rule by ID.
func (s *ToolRuleStore) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM tool_rules WHERE id = ?`, id)
	return err
}

// ForPrompt returns critical and high priority rules for a tool, sorted for prompt injection.
func (s *ToolRuleStore) ForPrompt(toolName string) ([]ToolRule, error) {
	rows, err := s.db.Query(
		`SELECT id, tool_name, content, priority, source, tags_json, created_at, updated_at
		 FROM tool_rules WHERE tool_name = ? AND priority IN ('critical', 'high')
		 ORDER BY CASE priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 END, created_at`, toolName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanToolRules(rows)
}

// ForPromptAll returns critical and high priority rules for all tools.
func (s *ToolRuleStore) ForPromptAll() ([]ToolRule, error) {
	rows, err := s.db.Query(
		`SELECT id, tool_name, content, priority, source, tags_json, created_at, updated_at
		 FROM tool_rules WHERE priority IN ('critical', 'high')
		 ORDER BY tool_name, CASE priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 END, created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanToolRules(rows)
}

// BuildPromptSection renders tool rules as a markdown block for system prompt injection.
func (s *ToolRuleStore) BuildPromptSection() string {
	rules, err := s.ForPromptAll()
	if err != nil || len(rules) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n## Tool-Specific Rules\n\n")

	currentTool := ""
	for _, r := range rules {
		if r.ToolName != currentTool {
			currentTool = r.ToolName
			b.WriteString(fmt.Sprintf("### %s\n", currentTool))
		}
		prefix := ""
		switch r.Priority {
		case RuleCritical:
			prefix = "[CRITICAL] "
		case RuleHigh:
			prefix = "[IMPORTANT] "
		}
		b.WriteString(fmt.Sprintf("- %s%s\n", prefix, r.Content))
	}
	return b.String()
}

func scanToolRules(rows *sql.Rows) ([]ToolRule, error) {
	var rules []ToolRule
	for rows.Next() {
		var r ToolRule
		var tagsJSON string
		if err := rows.Scan(&r.ID, &r.ToolName, &r.Content, &r.Priority, &r.Source, &tagsJSON, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(tagsJSON), &r.Tags)
		r.Priority = RulePriority(r.Priority)
		r.Source = RuleSource(r.Source)
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// randomSuffix returns a random hex string of the given byte length.
func randomSuffix(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based on entropy failure (extremely rare).
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
