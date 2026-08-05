package conversations

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Thread represents a conversation thread.
type Thread struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Labels        []string `json:"labels,omitempty"`
	PersonalityID string   `json:"personalityId,omitempty"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
}

// Message represents a single message within a thread.
type Message struct {
	ID           int64             `json:"id"`
	ThreadID     string            `json:"threadId"`
	Role         string            `json:"role"`
	Content      string            `json:"content"`
	ToolID       string            `json:"toolId,omitempty"`   // tool result correlation ID
	ToolCallJSON string            `json:"toolCall,omitempty"` // JSON of inference.ToolCall
	Metadata     map[string]string `json:"metadata,omitempty"`
	CreatedAt    string            `json:"createdAt"`
}

// Store persists threads and messages to SQLite.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	schema := `
	CREATE TABLE IF NOT EXISTS threads (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL DEFAULT '',
		labels TEXT NOT NULL DEFAULT '[]',
		personality_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		tool_id TEXT NOT NULL DEFAULT '',
		tool_call TEXT NOT NULL DEFAULT '',
		metadata TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages(thread_id, created_at);

		-- Episodic log: cross-session FTS5 search for conversation history.
		CREATE TABLE IF NOT EXISTS episodic_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			thread_id TEXT NOT NULL,
			message_id INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			tokens INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE VIRTUAL TABLE IF NOT EXISTS episodic_fts USING fts5(
			content, role, thread_id,
			content='episodic_log',
			content_rowid='id'
		);
		CREATE TRIGGER IF NOT EXISTS episodic_fts_ai AFTER INSERT ON episodic_log BEGIN
			INSERT INTO episodic_fts(rowid, content, role, thread_id)
			VALUES (new.id, new.content, new.role, new.thread_id);
		END;
		CREATE TRIGGER IF NOT EXISTS episodic_fts_ad AFTER DELETE ON episodic_log BEGIN
			INSERT INTO episodic_fts(episodic_fts, rowid, content, role, thread_id)
			VALUES ('delete', old.id, old.content, old.role, old.thread_id);
		END;
	`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("conversations schema: %w", err)
	}

	// Migrate older tables that may lack the labels / metadata columns.
	// Column may already exist — only treat non-duplicate errors as real failures.
	migrations := []string{
		"ALTER TABLE threads ADD COLUMN labels TEXT NOT NULL DEFAULT '[]'",
		"ALTER TABLE threads ADD COLUMN personality_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE messages ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'",
		"ALTER TABLE messages ADD COLUMN tool_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE messages ADD COLUMN tool_call TEXT NOT NULL DEFAULT ''",
	}
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			// SQLite returns "duplicate column name" when the column exists.
			// Any other error (disk full, corruption, lock) must propagate.
			errStr := err.Error()
			if !strings.Contains(strings.ToLower(errStr), "duplicate column") {
				return nil, fmt.Errorf("conversations migration (%q): %w", m, err)
			}
		}
	}

	return &Store{db: db}, nil
}

// ── Thread operations ──────────────────────────────────────────────

func (s *Store) CreateThread(id, title, personalityID string) error {
	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO threads (id, title, personality_id, updated_at) VALUES (?, ?, ?, datetime('now'))",
		id, title, personalityID,
	)
	return err
}

// EnsureThread creates the thread row if it doesn't exist, deriving the title
// from the first message. Idempotent — safe to call before every message.
func (s *Store) EnsureThread(id, firstMsg string) {
	title := SanitizeTitle(firstMsg)
	if title == "" {
		title = "New conversation"
	}
	_ = s.CreateThread(id, title, "")
}

func (s *Store) GetThread(id string) (*Thread, error) {
	var t Thread
	var labelsJSON string
	err := s.db.QueryRow(
		"SELECT id, title, labels, personality_id, created_at, updated_at FROM threads WHERE id = ?", id,
	).Scan(&t.ID, &t.Title, &labelsJSON, &t.PersonalityID, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if labelsJSON != "" {
		json.Unmarshal([]byte(labelsJSON), &t.Labels)
	}
	return &t, nil
}

func (s *Store) UpdateThreadTitle(id, title string) error {
	_, err := s.db.Exec(
		"UPDATE threads SET title = ?, updated_at = datetime('now') WHERE id = ?",
		title, id,
	)
	return err
}

func (s *Store) UpdateThreadLabels(id string, labels []string) error {
	data, err := json.Marshal(labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	_, err = s.db.Exec(
		"UPDATE threads SET labels = ?, updated_at = datetime('now') WHERE id = ?",
		string(data), id,
	)
	return err
}

func (s *Store) ListThreads(limit int) ([]Thread, error) {
	rows, err := s.db.Query(
		"SELECT id, title, labels, personality_id, created_at, updated_at FROM threads ORDER BY updated_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var threads []Thread
	for rows.Next() {
		var t Thread
		var labelsJSON string
		if err := rows.Scan(&t.ID, &t.Title, &labelsJSON, &t.PersonalityID, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		if labelsJSON != "" {
			json.Unmarshal([]byte(labelsJSON), &t.Labels)
		}
		threads = append(threads, t)
	}
	return threads, rows.Err()
}

func (s *Store) DeleteThread(id string) error {
	_, err := s.db.Exec("DELETE FROM messages WHERE thread_id = ?", id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("DELETE FROM threads WHERE id = ?", id)
	return err
}

func (s *Store) PurgeThreads() (int64, error) {
	res, err := s.db.Exec("DELETE FROM messages")
	if err != nil {
		return 0, err
	}
	mCount, _ := res.RowsAffected()
	res, err = s.db.Exec("DELETE FROM threads")
	if err != nil {
		return mCount, err
	}
	tCount, _ := res.RowsAffected()
	return mCount + tCount, nil
}

// ── Message operations ─────────────────────────────────────────────

func (s *Store) AddMessage(threadID, role, content string) error {
	return s.AddMessageWithTool(threadID, role, content, "", "")
}

// AddMessageWithTool persists a message with optional tool call / tool result metadata.
func (s *Store) AddMessageWithTool(threadID, role, content, toolID, toolCallJSON string) error {
	_, err := s.db.Exec(
		"INSERT INTO messages (thread_id, role, content, tool_id, tool_call) VALUES (?, ?, ?, ?, ?)",
		threadID, role, content, toolID, toolCallJSON,
	)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE threads SET updated_at = datetime('now') WHERE id = ?", threadID)
	return err
}

func (s *Store) AddMessageWithMeta(threadID, role, content string, meta map[string]string) error {
	metaJSON := "{}"
	if len(meta) > 0 {
		data, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
		metaJSON = string(data)
	}
	_, err := s.db.Exec(
		"INSERT INTO messages (thread_id, role, content, metadata) VALUES (?, ?, ?, ?)",
		threadID, role, content, metaJSON,
	)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE threads SET updated_at = datetime('now') WHERE id = ?", threadID)
	return err
}

// AddMessageFull persists a message with all fields including tool call context.
func (s *Store) AddMessageFull(threadID, role, content, toolID, toolCallJSON string, meta map[string]string) error {
	metaJSON := "{}"
	if len(meta) > 0 {
		data, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
		metaJSON = string(data)
	}
	_, err := s.db.Exec(
		"INSERT INTO messages (thread_id, role, content, tool_id, tool_call, metadata) VALUES (?, ?, ?, ?, ?, ?)",
		threadID, role, content, toolID, toolCallJSON, metaJSON,
	)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE threads SET updated_at = datetime('now') WHERE id = ?", threadID)
	return err
}

func (s *Store) GetMessages(threadID string, limit int) ([]Message, error) {
	rows, err := s.db.Query(
		"SELECT id, thread_id, role, content, tool_id, tool_call, metadata, created_at FROM messages WHERE thread_id = ? ORDER BY created_at ASC LIMIT ?",
		threadID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *Store) GetMessagesAfter(threadID string, afterID int64, limit int) ([]Message, error) {
	rows, err := s.db.Query(
		"SELECT id, thread_id, role, content, tool_id, tool_call, metadata, created_at FROM messages WHERE thread_id = ? AND id > ? ORDER BY created_at ASC LIMIT ?",
		threadID, afterID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *Store) GetMessage(id int64) (*Message, error) {
	var m Message
	var metaJSON string
	err := s.db.QueryRow(
		"SELECT id, thread_id, role, content, tool_id, tool_call, metadata, created_at FROM messages WHERE id = ?", id,
	).Scan(&m.ID, &m.ThreadID, &m.Role, &m.Content, &m.ToolID, &m.ToolCallJSON, &metaJSON, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if metaJSON != "" {
		json.Unmarshal([]byte(metaJSON), &m.Metadata)
	}
	return &m, nil
}

func (s *Store) UpdateMessage(id int64, content string, meta map[string]string) error {
	if content != "" {
		_, err := s.db.Exec("UPDATE messages SET content = ? WHERE id = ?", content, id)
		if err != nil {
			return err
		}
	}
	if len(meta) > 0 {
		data, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
		_, err = s.db.Exec("UPDATE messages SET metadata = ? WHERE id = ?", string(data), id)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteMessage(id int64) error {
	_, err := s.db.Exec("DELETE FROM messages WHERE id = ?", id)
	return err
}

func (s *Store) CountMessages(threadID string) (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM messages WHERE thread_id = ?", threadID).Scan(&count)
	return count, err
}

func (s *Store) SearchMessages(query string, limit int) ([]Message, error) {
	rows, err := s.db.Query(
		"SELECT id, thread_id, role, content, tool_id, tool_call, metadata, created_at FROM messages WHERE content LIKE ? ORDER BY created_at DESC LIMIT ?",
		"%"+query+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// ── Helpers ────────────────────────────────────────────────────────

func scanMessages(rows *sql.Rows) ([]Message, error) {
	var msgs []Message
	for rows.Next() {
		var m Message
		var metaJSON string
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Content, &m.ToolID, &m.ToolCallJSON, &metaJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		if metaJSON != "" {
			json.Unmarshal([]byte(metaJSON), &m.Metadata)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// SanitizeTitle collapses whitespace, strips quotes and trailing punctuation, truncates to 80 chars.
func SanitizeTitle(raw string) string {
	t := strings.TrimSpace(raw)
	// take first line
	if idx := strings.IndexAny(t, "\n\r"); idx >= 0 {
		t = t[:idx]
	}
	// strip wrapping quotes / backticks
	for (strings.HasPrefix(t, `"`) && strings.HasSuffix(t, `"`)) ||
		(strings.HasPrefix(t, "'") && strings.HasSuffix(t, "'")) ||
		(strings.HasPrefix(t, "`") && strings.HasSuffix(t, "`")) {
		t = t[1 : len(t)-1]
	}
	// drop trailing punctuation
	t = strings.TrimRight(t, ".,;:!?。，；：！？")
	t = strings.TrimSpace(t)

	// collapse whitespace
	words := strings.Fields(t)
	t = strings.Join(words, " ")

	if len(t) > 80 {
		t = t[:80]
	}
	return t
}

// AppendEpisodic inserts a message into the episodic log for cross-session search.
func (s *Store) AppendEpisodic(threadID string, messageID int64, role, content string, tokens int) error {
	_, err := s.db.Exec(
		"INSERT INTO episodic_log (thread_id, message_id, role, content, tokens) VALUES (?, ?, ?, ?, ?)",
		threadID, messageID, role, content, tokens,
	)
	return err
}

// EpisodicResult is a search result from the episodic log.
type EpisodicResult struct {
	ThreadID  string
	Role      string
	Content   string
	Tokens    int
	CreatedAt string
}

// SearchEpisodic performs cross-session FTS5 search on conversation history.
// excludeThreadID can be used to skip the current conversation.
func (s *Store) SearchEpisodic(query string, excludeThreadID string, limit int) ([]EpisodicResult, error) {
	ftsQuery := escapeEpisodicQuery(query)
	var rows *sql.Rows
	var err error
	if excludeThreadID != "" {
		rows, err = s.db.Query(
			`SELECT e.thread_id, e.role, e.content, e.tokens, e.created_at
			 FROM episodic_log e JOIN episodic_fts fts ON e.id = fts.rowid
			 WHERE episodic_fts MATCH ? AND e.thread_id != ?
			 ORDER BY rank LIMIT ?`,
			ftsQuery, excludeThreadID, limit,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT e.thread_id, e.role, e.content, e.tokens, e.created_at
			 FROM episodic_log e JOIN episodic_fts fts ON e.id = fts.rowid
			 WHERE episodic_fts MATCH ?
			 ORDER BY rank LIMIT ?`,
			ftsQuery, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []EpisodicResult
	for rows.Next() {
		var r EpisodicResult
		if err := rows.Scan(&r.ThreadID, &r.Role, &r.Content, &r.Tokens, &r.CreatedAt); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func escapeEpisodicQuery(q string) string {
	// Only strip characters that break FTS5 query syntax — do NOT strip
	// non-Latin characters. The episodic_fts table uses the unicode61
	// tokenizer which handles CJK, Arabic, Cyrillic, etc. natively.
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
