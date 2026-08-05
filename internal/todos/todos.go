package todos

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Status is the lifecycle state of a task card.
type Status string

const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusBlocked    Status = "blocked"
	StatusDone       Status = "done"
)

// Card is a single task on the kanban board.
type Card struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     Status `json:"status"`
	Order      int    `json:"order"`
	Notes      string `json:"notes,omitempty"`
	AssignedTo string `json:"assignedTo,omitempty"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

// Board is the full kanban board for a thread.
type Board struct {
	ThreadID string `json:"threadId"`
	Cards    []Card `json:"cards"`
}

// Snapshot is a board + rendered markdown summary.
type Snapshot struct {
	ThreadID string `json:"threadId"`
	Cards    []Card `json:"cards"`
	Markdown string `json:"markdown"`
}

// Store persists boards as JSON files per thread.
type Store struct {
	mu  sync.Mutex
	dir string
}

// NewStore creates a board store rooted at the workspace directory.
func NewStore(workspaceDir string) *Store {
	dir := filepath.Join(workspaceDir, "agent_task_boards")
	os.MkdirAll(dir, 0755)
	return &Store{dir: dir}
}

func (s *Store) pathFor(threadID string) string {
	return filepath.Join(s.dir, threadID+".json")
}

// ── CRUD ───────────────────────────────────────────────────────────

// GetBoard loads the board for a thread, returning an empty board if absent.
func (s *Store) GetBoard(threadID string) (*Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readBoard(threadID)
}

// readBoard loads the board without locking (caller must hold the lock).
func (s *Store) readBoard(threadID string) (*Board, error) {
	b := &Board{ThreadID: threadID, Cards: make([]Card, 0)}
	data, err := os.ReadFile(s.pathFor(threadID))
	if os.IsNotExist(err) {
		return b, nil
	}
	if err != nil {
		return b, fmt.Errorf("read board: %w", err)
	}
	if err := json.Unmarshal(data, b); err != nil {
		return b, fmt.Errorf("unmarshal board: %w", err)
	}
	if b.Cards == nil {
		b.Cards = make([]Card, 0)
	}
	return b, nil
}

func (s *Store) saveBoard(b *Board) error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.pathFor(b.ThreadID) + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.pathFor(b.ThreadID))
}

// List returns cards for a thread, sorted by order.
func (s *Store) List(threadID string) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := s.readBoard(threadID)
	if err != nil {
		return nil, err
	}
	sortCards(b.Cards)
	return &Snapshot{
		ThreadID: threadID,
		Cards:    b.Cards,
		Markdown: renderMarkdown(b.Cards),
	}, nil
}

// Add creates a new card on the board.
func (s *Store) Add(threadID, title, notes string) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := s.readBoard(threadID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	nextOrder := 0
	for _, c := range b.Cards {
		if c.Order >= nextOrder {
			nextOrder = c.Order + 1
		}
	}

	card := Card{
		ID:        fmt.Sprintf("card-%d", time.Now().UnixNano()),
		Title:     title,
		Status:    StatusTodo,
		Order:     nextOrder,
		Notes:     notes,
		CreatedAt: now,
		UpdatedAt: now,
	}
	b.Cards = append(b.Cards, card)

	if err := s.saveBoard(b); err != nil {
		return nil, err
	}

	sortCards(b.Cards)
	return &Snapshot{
		ThreadID: threadID,
		Cards:    b.Cards,
		Markdown: renderMarkdown(b.Cards),
	}, nil
}

// Edit updates a card's title and notes.
func (s *Store) Edit(threadID, cardID, title, notes string) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := s.readBoard(threadID)
	if err != nil {
		return nil, err
	}

	for i, c := range b.Cards {
		if c.ID == cardID {
			if title != "" {
				b.Cards[i].Title = title
			}
			if notes != "" {
				b.Cards[i].Notes = notes
			}
			b.Cards[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			break
		}
	}

	if err := s.saveBoard(b); err != nil {
		return nil, err
	}

	sortCards(b.Cards)
	return &Snapshot{
		ThreadID: threadID,
		Cards:    b.Cards,
		Markdown: renderMarkdown(b.Cards),
	}, nil
}

// UpdateStatus moves a card to a new status.
func (s *Store) UpdateStatus(threadID, cardID string, status Status) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := s.readBoard(threadID)
	if err != nil {
		return nil, err
	}

	found := false
	for i, c := range b.Cards {
		if c.ID == cardID {
			b.Cards[i].Status = status
			b.Cards[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("card %s not found", cardID)
	}

	if err := s.saveBoard(b); err != nil {
		return nil, err
	}

	sortCards(b.Cards)
	return &Snapshot{
		ThreadID: threadID,
		Cards:    b.Cards,
		Markdown: renderMarkdown(b.Cards),
	}, nil
}

// Remove deletes a card from the board.
func (s *Store) Remove(threadID, cardID string) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := s.readBoard(threadID)
	if err != nil {
		return nil, err
	}

	filtered := make([]Card, 0, len(b.Cards))
	for _, c := range b.Cards {
		if c.ID != cardID {
			filtered = append(filtered, c)
		}
	}
	b.Cards = filtered

	if err := s.saveBoard(b); err != nil {
		return nil, err
	}

	sortCards(b.Cards)
	return &Snapshot{
		ThreadID: threadID,
		Cards:    b.Cards,
		Markdown: renderMarkdown(b.Cards),
	}, nil
}

// Clear removes all cards from a thread's board.
func (s *Store) Clear(threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(s.pathFor(threadID))
}

// ── Helpers ────────────────────────────────────────────────────────

func sortCards(cards []Card) {
	sort.Slice(cards, func(i, j int) bool {
		return cards[i].Order < cards[j].Order
	})
}

func renderMarkdown(cards []Card) string {
	if len(cards) == 0 {
		return "No tasks."
	}

	byStatus := map[Status][]Card{
		StatusTodo:       {},
		StatusInProgress: {},
		StatusBlocked:    {},
		StatusDone:       {},
	}

	for _, c := range cards {
		byStatus[c.Status] = append(byStatus[c.Status], c)
	}

	var out string
	sections := []struct {
		status Status
		header string
	}{
		{StatusTodo, "## Todo"},
		{StatusInProgress, "## In Progress"},
		{StatusBlocked, "## Blocked"},
		{StatusDone, "## Done"},
	}

	for _, sec := range sections {
		cards := byStatus[sec.status]
		if len(cards) == 0 {
			continue
		}
		out += sec.header + "\n\n"
		for _, c := range cards {
			out += fmt.Sprintf("- [ ] **%s**", c.Title)
			if c.Notes != "" {
				out += fmt.Sprintf(" — %s", c.Notes)
			}
			out += "\n"
		}
		out += "\n"
	}

	return out
}
