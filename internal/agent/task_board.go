package agent

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// TaskCardStatus represents the lifecycle of a task card in the kanban board.
type TaskCardStatus string

const (
	StatusTodo             TaskCardStatus = "todo"
	StatusAwaitingApproval TaskCardStatus = "awaiting_approval"
	StatusReady            TaskCardStatus = "ready"
	StatusInProgress       TaskCardStatus = "in_progress"
	StatusBlocked          TaskCardStatus = "blocked"
	StatusDone             TaskCardStatus = "done"
	StatusRejected         TaskCardStatus = "rejected"
)

// TaskCard is a single work item on the agent's task board.
type TaskCard struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Status      TaskCardStatus `json:"status"`
	Priority    string         `json:"priority"` // critical, high, normal, low
	Assignee    string         `json:"assignee,omitempty"`
	ParentID    string         `json:"parent_id,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`

	// Source metadata for ingested cards from external task sources.
	SourceMeta *TaskSourceMeta `json:"source_meta,omitempty"`

	// Session thread for autonomous runs.
	SessionThreadID string `json:"session_thread_id,omitempty"`

	// ClaimedBy tracks which execution run currently owns this card.
	ClaimedBy string     `json:"claimed_by,omitempty"`
	ClaimedAt *time.Time `json:"claimed_at,omitempty"`
}

// TaskSourceMeta carries origin information for externally ingested cards.
type TaskSourceMeta struct {
	Provider   string `json:"provider,omitempty"`    // e.g. "github", "linear", "cron"
	SourceID   string `json:"source_id,omitempty"`   // external ID in the source system
	ExternalID string `json:"external_id,omitempty"` // alternative external identifier
	URL        string `json:"url,omitempty"`         // link to the source item
	Urgency    int    `json:"urgency,omitempty"`     // 0-10 urgency score from source
}

// TaskBoard is a kanban-style task tracker for the agent's work items.
type TaskBoard struct {
	mu    sync.RWMutex
	cards map[string]*TaskCard
	order []string // card IDs in display order
}

// NewTaskBoard creates a new task board.
func NewTaskBoard() *TaskBoard {
	return &TaskBoard{
		cards: make(map[string]*TaskCard),
	}
}

// Add creates a new task card.
func (b *TaskBoard) Add(title, description, priority string) *TaskCard {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := fmt.Sprintf("task_%d", time.Now().UnixNano())
	card := &TaskCard{
		ID:          id,
		Title:       title,
		Description: description,
		Status:      StatusTodo,
		Priority:    priority,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	b.cards[id] = card
	b.order = append(b.order, id)
	return card
}

// Get returns a card by ID.
func (b *TaskBoard) Get(id string) *TaskCard {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cards[id]
}

// UpdateStatus transitions a card to a new status.
func (b *TaskBoard) UpdateStatus(id string, status TaskCardStatus) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	card, ok := b.cards[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	if !isValidTransition(card.Status, status) {
		return fmt.Errorf("invalid status transition: %s → %s", card.Status, status)
	}
	card.Status = status
	card.UpdatedAt = time.Now().UTC()
	if status == StatusDone || status == StatusRejected {
		now := time.Now().UTC()
		card.CompletedAt = &now
	}
	return nil
}

// List returns cards filtered by status.
func (b *TaskBoard) List(status TaskCardStatus) []*TaskCard {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var result []*TaskCard
	for _, id := range b.order {
		card := b.cards[id]
		if status == "" || card.Status == status {
			result = append(result, card)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		pi := priorityWeight(result[i].Priority)
		pj := priorityWeight(result[j].Priority)
		if pi != pj {
			return pi < pj
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

// All returns all cards.
func (b *TaskBoard) All() []*TaskCard { return b.List("") }

// FormatBoard returns a markdown representation of the task board.
func (b *TaskBoard) FormatBoard() string {
	var bld strings.Builder
	bld.WriteString("## Task Board\n\n")

	columns := []struct {
		status TaskCardStatus
		header string
	}{
		{StatusTodo, "### Todo"},
		{StatusAwaitingApproval, "### Awaiting Approval"},
		{StatusInProgress, "### In Progress"},
		{StatusBlocked, "### Blocked"},
		{StatusDone, "### Done"},
	}

	for _, col := range columns {
		cards := b.List(col.status)
		if len(cards) == 0 {
			continue
		}
		bld.WriteString(col.header + "\n\n")
		for _, c := range cards {
			bld.WriteString(fmt.Sprintf("- [%s] **%s** [%s]\n", c.ID, c.Title, c.Priority))
			if c.Description != "" {
				bld.WriteString(fmt.Sprintf("  %s\n", truncateDescription(c.Description, 100)))
			}
		}
		bld.WriteString("\n")
	}
	return bld.String()
}

func isValidTransition(from, to TaskCardStatus) bool {
	// Allow any transition except from Done/Rejected to active states.
	if from == StatusDone || from == StatusRejected {
		return to == StatusDone || to == StatusRejected
	}
	return true
}

func priorityWeight(p string) int {
	switch p {
	case "critical":
		return 0
	case "high":
		return 1
	case "normal":
		return 2
	case "low":
		return 3
	default:
		return 2
	}
}

func truncateDescription(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
