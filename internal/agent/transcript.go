package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/simon/mneme/internal/inference"
)

// sanitizeSessionID replaces path separators with safe dashes to prevent
// path traversal when session IDs are used in file paths.
func sanitizeSessionID(id string) string {
	safe := strings.ReplaceAll(id, "/", "-")
	safe = strings.ReplaceAll(safe, "\\", "-")
	safe = strings.ReplaceAll(safe, "..", "--")
	safe = filepath.Base(safe) // strip any directory components
	if safe == "." || safe == "" {
		return "unknown"
	}
	return safe
}

// TranscriptEntry is a single line in the session transcript.
type TranscriptEntry struct {
	Timestamp string `json:"timestamp"`
	Role      string `json:"role"`
	Content   string `json:"content,omitempty"`
	ToolCall  *struct {
		Name string `json:"name"`
		Args string `json:"arguments"`
	} `json:"tool_call,omitempty"`
}

// WriteTranscript writes a JSONL transcript entry for a session message.
func WriteTranscript(workspaceDir, sessionID, role, content string, toolCall *inference.ToolCall) error {
	sessionID = sanitizeSessionID(sessionID)
	dir := filepath.Join(workspaceDir, "transcripts")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create transcript dir: %w", err)
	}

	entry := TranscriptEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Role:      role,
		Content:   content,
	}
	if toolCall != nil {
		entry.ToolCall = &struct {
			Name string `json:"name"`
			Args string `json:"arguments"`
		}{
			Name: toolCall.Name,
			Args: string(toolCall.Arguments),
		}
	}

	f, err := os.OpenFile(filepath.Join(dir, sessionID+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal transcript entry: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write transcript: %w", err)
	}
	return nil
}

// ReadTranscript loads the last N entries from a session transcript file.
// When limit > 0, uses a ring buffer to avoid loading the entire file into memory
// for large transcript files.
func ReadTranscript(workspaceDir, sessionID string, limit int) ([]TranscriptEntry, error) {
	sessionID = sanitizeSessionID(sessionID)
	path := filepath.Join(workspaceDir, "transcripts", sessionID+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	if limit <= 0 {
		var entries []TranscriptEntry
		decoder := json.NewDecoder(f)
		for decoder.More() {
			var entry TranscriptEntry
			if err := decoder.Decode(&entry); err != nil {
				continue
			}
			entries = append(entries, entry)
		}
		return entries, nil
	}

	// Ring buffer: only retain the last `limit` entries.
	ring := make([]TranscriptEntry, limit)
	idx := 0
	count := 0
	decoder := json.NewDecoder(f)
	for decoder.More() {
		var entry TranscriptEntry
		if err := decoder.Decode(&entry); err != nil {
			continue
		}
		ring[idx%limit] = entry
		idx++
		count++
	}

	if count <= limit {
		return ring[:count], nil
	}

	// Rotate so entries are in chronological order.
	start := idx % limit
	result := make([]TranscriptEntry, limit)
	copy(result, ring[start:])
	copy(result[limit-start:], ring[:start])
	return result, nil
}

// DeleteTranscript removes a session transcript file.
func DeleteTranscript(workspaceDir, sessionID string) error {
	sessionID = sanitizeSessionID(sessionID)
	path := filepath.Join(workspaceDir, "transcripts", sessionID+".jsonl")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
