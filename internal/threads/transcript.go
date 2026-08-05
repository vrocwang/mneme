// Package threads provides session transcript persistence matching Rust's
// TurnStateStore + TranscriptObserver. Writes per-turn JSONL records and
// human-readable .md transcripts to <workspace>/memory/conversations/.
package threads

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TurnRecord is one turn in a conversation transcript, matching Rust TurnState.
type TurnRecord struct {
	TurnID       string           `json:"turn_id"`
	ThreadID     string           `json:"thread_id"`
	Role         string           `json:"role"`
	Content      string           `json:"content"`
	ToolCalls    []ToolCallRecord `json:"tool_calls,omitempty"`
	Timestamp    time.Time        `json:"timestamp"`
	InputTokens  int              `json:"input_tokens"`
	OutputTokens int              `json:"output_tokens"`
	DurationMs   int64            `json:"duration_ms"`
	Model        string           `json:"model,omitempty"`
}

// ToolCallRecord is one tool invocation within a turn.
type ToolCallRecord struct {
	Name       string `json:"name"`
	Success    bool   `json:"success"`
	OutputLen  int    `json:"output_len"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// TranscriptSummary is metadata about a persisted transcript.
type TranscriptSummary struct {
	ThreadID  string    `json:"thread_id"`
	TurnCount int       `json:"turn_count"`
	FirstTurn time.Time `json:"first_turn"`
	LastTurn  time.Time `json:"last_turn"`
}

// TranscriptStore persists turns as JSONL + .md files.
type TranscriptStore struct {
	workspaceDir string
}

// NewTranscriptStore creates a transcript store rooted at workspace/memory/conversations.
func NewTranscriptStore(workspaceDir string) *TranscriptStore {
	return &TranscriptStore{workspaceDir: workspaceDir}
}

func (s *TranscriptStore) dir() string {
	return filepath.Join(s.workspaceDir, "memory", "conversations")
}

func (s *TranscriptStore) jsonlPath(threadID string) string {
	return filepath.Join(s.dir(), threadID+".jsonl")
}

func (s *TranscriptStore) mdPath(threadID string) string {
	return filepath.Join(s.dir(), threadID+".md")
}

// AppendTurn writes one turn record to the JSONL file and updates the .md transcript.
func (s *TranscriptStore) AppendTurn(record TurnRecord) error {
	if err := os.MkdirAll(s.dir(), 0755); err != nil {
		return fmt.Errorf("transcript: create dir: %w", err)
	}

	// Append JSONL.
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.jsonlPath(record.ThreadID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}

	// Update .md.
	return s.writeMarkdown(record.ThreadID)
}

// writeMarkdown regenerates the full .md transcript from the JSONL.
func (s *TranscriptStore) writeMarkdown(threadID string) error {
	records, err := s.ReadTranscript(threadID)
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Conversation: %s\n\n", threadID))
	b.WriteString(fmt.Sprintf("Turns: %d | Last updated: %s\n\n", len(records), time.Now().Format(time.RFC3339)))
	b.WriteString("---\n\n")

	for _, r := range records {
		b.WriteString(fmt.Sprintf("## Turn — %s\n\n", r.Timestamp.Format(time.RFC3339)))
		b.WriteString(fmt.Sprintf("**%s** (model: %s, %d→%d tokens, %dms)\n\n", r.Role, r.Model, r.InputTokens, r.OutputTokens, r.DurationMs))
		b.WriteString(r.Content)
		b.WriteString("\n\n")
		if len(r.ToolCalls) > 0 {
			b.WriteString("### Tool Calls\n\n")
			for _, tc := range r.ToolCalls {
				status := "OK"
				if !tc.Success {
					status = "FAILED"
				}
				b.WriteString(fmt.Sprintf("- `%s` [%s] %dms", tc.Name, status, tc.DurationMs))
				if tc.Error != "" {
					b.WriteString(fmt.Sprintf(" — %s", tc.Error))
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
		b.WriteString("---\n\n")
	}

	return os.WriteFile(s.mdPath(threadID), []byte(b.String()), 0644)
}

// ReadTranscript reads all turns from the JSONL file.
func (s *TranscriptStore) ReadTranscript(threadID string) ([]TurnRecord, error) {
	data, err := os.ReadFile(s.jsonlPath(threadID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var records []TurnRecord
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r TurnRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		records = append(records, r)
	}
	return records, nil
}

// ListThreads returns metadata for all persisted threads.
func (s *TranscriptStore) ListThreads() ([]TranscriptSummary, error) {
	entries, err := os.ReadDir(s.dir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var summaries []TranscriptSummary
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		threadID := strings.TrimSuffix(e.Name(), ".jsonl")
		records, err := s.ReadTranscript(threadID)
		if err != nil || len(records) == 0 {
			continue
		}
		summaries = append(summaries, TranscriptSummary{
			ThreadID:  threadID,
			TurnCount: len(records),
			FirstTurn: records[0].Timestamp,
			LastTurn:  records[len(records)-1].Timestamp,
		})
	}
	return summaries, nil
}

// TranscriptObserver wraps TranscriptStore as a PostTurnHook-compatible observer.
type TranscriptObserver struct {
	store *TranscriptStore
	model string
}

// NewTranscriptObserver creates an observer that persists turn records.
func NewTranscriptObserver(store *TranscriptStore, model string) *TranscriptObserver {
	return &TranscriptObserver{store: store, model: model}
}

// OnTurnComplete persists a turn to the transcript store.
func (o *TranscriptObserver) OnTurnComplete(threadID string, role string, content string, toolCalls []ToolCallRecord, inputTokens, outputTokens int, durationMs int64) {
	o.store.AppendTurn(TurnRecord{
		TurnID:       fmt.Sprintf("turn_%d", time.Now().UnixNano()),
		ThreadID:     threadID,
		Role:         role,
		Content:      content,
		ToolCalls:    toolCalls,
		Timestamp:    time.Now(),
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		DurationMs:   durationMs,
		Model:        o.model,
	})
}
