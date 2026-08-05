package subconscious

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ScratchpadEntry is a persistent note in the subconscious working memory.
// Matches Rust's ScratchpadEntry in scratchpad/mod.rs.
type ScratchpadEntry struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	Priority  int       `json:"priority"` // 0-10, higher = more important
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const (
	scratchpadFileName     = "SUBCONSCIOUS_SCRATCHPAD.md"
	defaultMaxEntries      = 100
	minPriorityForEviction = 3 // entries below this priority are evicted first
)

// Scratchpad manages persistent working memory across subconscious ticks.
// Stored as a markdown file with embedded JSON for cross-tick continuity.
// Matches Rust's scratchpad subsystem (scratchpad/mod.rs).
type Scratchpad struct {
	workspaceDir string
	entries      []ScratchpadEntry
	maxEntries   int
}

// NewScratchpad creates or loads a scratchpad from the workspace directory.
func NewScratchpad(workspaceDir string) *Scratchpad {
	return &Scratchpad{
		workspaceDir: workspaceDir,
		maxEntries:   defaultMaxEntries,
	}
}

// Load reads the scratchpad file from disk. Creates an empty file if none exists.
func (s *Scratchpad) Load() error {
	path := s.filePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.entries = nil
			return nil
		}
		return fmt.Errorf("scratchpad load: %w", err)
	}

	// Extract JSON from markdown code fence.
	content := string(data)
	start := strings.Index(content, "```json")
	if start < 0 {
		s.entries = nil
		return nil
	}
	start += 7 // skip ```json
	end := strings.Index(content[start:], "```")
	if end < 0 {
		s.entries = nil
		return nil
	}
	jsonStr := content[start : start+end]

	var entries []ScratchpadEntry
	if err := json.Unmarshal([]byte(jsonStr), &entries); err != nil {
		// Fallback: try parsing as top-level JSON array.
		s.entries = nil
		return nil
	}
	s.entries = entries
	return nil
}

// Save persists the scratchpad to disk as a markdown file with embedded JSON.
func (s *Scratchpad) Save() error {
	path := s.filePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("scratchpad mkdir: %w", err)
	}

	jsonBytes, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("scratchpad marshal: %w", err)
	}

	content := fmt.Sprintf("# Subconscious Scratchpad\n\n```json\n%s\n```\n", string(jsonBytes))
	return os.WriteFile(path, []byte(content), 0644)
}

// Add appends a new entry with an auto-generated short ID. Evicts oldest
// low-priority entries when over capacity.
func (s *Scratchpad) Add(body string, priority int) *ScratchpadEntry {
	now := time.Now()
	entry := ScratchpadEntry{
		ID:        generateShortID(),
		Body:      body,
		Priority:  priority,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.entries = append(s.entries, entry)
	s.evict()
	return &entry
}

// Remove deletes an entry by ID.
func (s *Scratchpad) Remove(id string) bool {
	for i, e := range s.entries {
		if e.ID == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return true
		}
	}
	return false
}

// Edit updates an entry's body and optional priority, bumping UpdatedAt.
func (s *Scratchpad) Edit(id, body string, priority int) bool {
	for i, e := range s.entries {
		if e.ID == id {
			s.entries[i].Body = body
			s.entries[i].UpdatedAt = time.Now()
			if priority >= 0 {
				s.entries[i].Priority = priority
			}
			return true
		}
	}
	return false
}

// RenderForPrompt returns the scratchpad entries formatted for LLM injection.
// Higher priority entries come first. Truncates entry bodies to maxLen chars.
func (s *Scratchpad) RenderForPrompt(maxLen int) string {
	if len(s.entries) == 0 {
		return ""
	}

	// Sort by priority descending, then updated_at descending.
	sorted := make([]ScratchpadEntry, len(s.entries))
	copy(sorted, s.entries)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority > sorted[j].Priority
		}
		return sorted[i].UpdatedAt.After(sorted[j].UpdatedAt)
	})

	var b strings.Builder
	b.WriteString("## Scratchpad (Persistent Working Memory)\n\n")
	b.WriteString("You maintain this scratchpad across ticks. High-priority entries appear first.\n")
	b.WriteString("Suggest additions/removals in your JSON output using: ")
	b.WriteString(`{"type":"scratchpad_add","body":"...","priority":5} or {"type":"scratchpad_remove","id":"..."}`)
	b.WriteString("\n\n")

	for _, e := range sorted {
		body := e.Body
		if maxLen > 0 && len(body) > maxLen {
			body = body[:maxLen] + "..."
		}
		b.WriteString(fmt.Sprintf("- [%s] (p%d) %s\n", e.ID, e.Priority, body))
	}
	b.WriteString("\n")
	return b.String()
}

// MergeActions processes scratchpad directives from LLM output Actions.
// Returns new Actions for any scratchpad operations that produce notifications.
func (s *Scratchpad) MergeActions(actions []Action) []Action {
	changed := false
	for _, a := range actions {
		switch a.Type {
		case "scratchpad_add":
			body, _ := a.Payload["body"].(string)
			priority := 5
			if p, ok := a.Payload["priority"].(float64); ok {
				priority = int(p)
			}
			if body != "" {
				s.Add(body, priority)
				changed = true
			}
		case "scratchpad_remove":
			if id, ok := a.Payload["id"].(string); ok && id != "" {
				s.Remove(id)
				changed = true
			}
		}
	}
	if changed {
		if err := s.Save(); err != nil {
			return actions // save failure is non-fatal
		}
	}

	// Filter out scratchpad directives — they're not user-visible actions.
	filtered := make([]Action, 0, len(actions))
	for _, a := range actions {
		if a.Type == "scratchpad_add" || a.Type == "scratchpad_remove" {
			continue
		}
		filtered = append(filtered, a)
	}
	return filtered
}

func (s *Scratchpad) Len() int { return len(s.entries) }

func (s *Scratchpad) filePath() string {
	return filepath.Join(s.workspaceDir, "memory", scratchpadFileName)
}

func (s *Scratchpad) evict() {
	if len(s.entries) <= s.maxEntries {
		return
	}
	// Sort by priority ascending, then updated_at ascending.
	sort.Slice(s.entries, func(i, j int) bool {
		if s.entries[i].Priority != s.entries[j].Priority {
			return s.entries[i].Priority < s.entries[j].Priority
		}
		return s.entries[i].UpdatedAt.Before(s.entries[j].UpdatedAt)
	})
	// Remove oldest low-priority entries until under capacity.
	excess := len(s.entries) - s.maxEntries
	for i := 0; i < excess && len(s.entries) > 0; i++ {
		if s.entries[0].Priority >= minPriorityForEviction {
			break // don't evict important entries
		}
		s.entries = s.entries[1:]
	}
}

var scratchpadChars = "abcdefghijklmnopqrstuvwxyz0123456789"

func generateShortID() string {
	var b strings.Builder
	b.Grow(6)
	for i := 0; i < 6; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(36))
		if err != nil {
			// crypto/rand failure is catastrophic; fall back to timestamp.
			b.WriteByte(scratchpadChars[time.Now().UnixNano()%36])
			continue
		}
		b.WriteByte(scratchpadChars[n.Int64()])
	}
	return b.String()
}
