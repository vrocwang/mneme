package threads

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/simon/mneme/internal/memory/conversations"
)

// Ops provides thread business logic backed by a conversations store.
type Ops struct {
	store *conversations.Store
}

// NewOps creates a new Ops instance.
func NewOps(store *conversations.Store) *Ops {
	return &Ops{store: store}
}

// ── Thread operations ─────────────────────────────────────────────

// List returns all thread summaries, newest first.
func (o *Ops) List(limit int) ([]ThreadSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	threads, err := o.store.ListThreads(limit)
	if err != nil {
		return nil, fmt.Errorf("list threads: %w", err)
	}
	result := make([]ThreadSummary, len(threads))
	for i, t := range threads {
		count, _ := o.store.CountMessages(t.ID)
		result[i] = ThreadSummary{
			ID:           t.ID,
			Title:        t.Title,
			Labels:       t.Labels,
			MessageCount: count,
			CreatedAt:    t.CreatedAt,
			UpdatedAt:    t.UpdatedAt,
		}
		if result[i].Labels == nil {
			result[i].Labels = []string{}
		}
	}
	return result, nil
}

// CreateNew creates a new thread with an auto-generated ID.
// If title is empty, a default placeholder is used.
func (o *Ops) CreateNew(title string, labels []string, personalityID string) (*ThreadSummary, error) {
	id := "thread-" + uuid.New().String()[:8]
	if title == "" {
		title = DefaultThreadTitle()
	}
	if err := o.store.CreateThread(id, title, personalityID); err != nil {
		return nil, fmt.Errorf("create thread: %w", err)
	}
	if len(labels) > 0 {
		o.store.UpdateThreadLabels(id, labels)
	}
	return &ThreadSummary{
		ID:            id,
		Title:         title,
		Labels:        labels,
		PersonalityID: personalityID,
		MessageCount:  0,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// Upsert creates or refreshes a thread.
func (o *Ops) Upsert(id, title string, labels []string, personalityID string) (*ThreadSummary, error) {
	if id == "" {
		return nil, fmt.Errorf("thread id is required")
	}
	if title == "" {
		title = DefaultThreadTitle()
	}

	if err := o.store.CreateThread(id, title, personalityID); err != nil {
		return nil, fmt.Errorf("upsert thread: %w", err)
	}

	// update title if different from placeholder
	existing, _ := o.store.GetThread(id)
	if existing != nil && title != existing.Title && title != DefaultThreadTitle() {
		o.store.UpdateThreadTitle(id, title)
	}
	if len(labels) > 0 {
		o.store.UpdateThreadLabels(id, labels)
	}

	t, err := o.store.GetThread(id)
	if err != nil || t == nil {
		return nil, fmt.Errorf("thread not found after upsert")
	}
	count, _ := o.store.CountMessages(id)

	return &ThreadSummary{
		ID:            t.ID,
		Title:         t.Title,
		Labels:        t.Labels,
		PersonalityID: t.PersonalityID,
		MessageCount:  count,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
	}, nil
}

// UpdateTitle sets a user-specified title. Rejects empty/whitespace-only.
func (o *Ops) UpdateTitle(threadID, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("title must not be empty")
	}
	return o.store.UpdateThreadTitle(threadID, title)
}

// UpdateLabels replaces labels for a thread.
func (o *Ops) UpdateLabels(threadID string, labels []string) error {
	return o.store.UpdateThreadLabels(threadID, labels)
}

// Delete removes a thread and all its messages.
func (o *Ops) Delete(threadID string) error {
	t, err := o.store.GetThread(threadID)
	if err != nil {
		return fmt.Errorf("delete thread: %w", err)
	}
	if t == nil {
		return fmt.Errorf("thread %s not found", threadID)
	}
	return o.store.DeleteThread(threadID)
}

// Purge removes all threads and messages.
func (o *Ops) Purge() (int64, error) {
	return o.store.PurgeThreads()
}

// ── Message operations ─────────────────────────────────────────────

// ListMessages returns messages for a thread.
func (o *Ops) ListMessages(threadID string, limit int, afterID int64) ([]MessageRecord, error) {
	if limit <= 0 {
		limit = 100
	}

	var msgs []conversations.Message
	var err error

	if afterID > 0 {
		msgs, err = o.store.GetMessagesAfter(threadID, afterID, limit)
	} else {
		msgs, err = o.store.GetMessages(threadID, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	result := make([]MessageRecord, len(msgs))
	for i, m := range msgs {
		result[i] = MessageRecord{
			ID:        m.ID,
			ThreadID:  m.ThreadID,
			Role:      m.Role,
			Content:   m.Content,
			Metadata:  m.Metadata,
			CreatedAt: m.CreatedAt,
		}
	}
	return result, nil
}

// AppendMessage persists a message to a thread. Returns ErrThreadNotFound if the
// thread does not exist.
func (o *Ops) AppendMessage(threadID, role, content string, meta map[string]string) (*MessageRecord, error) {
	t, err := o.store.GetThread(threadID)
	if err != nil {
		return nil, fmt.Errorf("append message: %w", err)
	}
	if t == nil {
		return nil, ErrThreadNotFound(threadID)
	}
	if err := o.store.AddMessageWithMeta(threadID, role, content, meta); err != nil {
		return nil, fmt.Errorf("append message: %w", err)
	}
	return &MessageRecord{
		ThreadID: threadID,
		Role:     role,
		Content:  content,
		Metadata: meta,
	}, nil
}

// UpdateMessage patches a message's content and/or metadata.
func (o *Ops) UpdateMessage(messageID int64, content string, meta map[string]string) error {
	return o.store.UpdateMessage(messageID, content, meta)
}

// ── Title generation ──────────────────────────────────────────────

// TitleGenerator is the interface for LLM-based title generation.
type TitleGenerator interface {
	GenerateTitle(userMessage, assistantMessage string) (string, error)
}

// GenerateTitle generates a title for the thread. If the thread already has a
// user-set (non-auto-generated) title, it is not overwritten. Falls back to a
// deterministic title derived from the first user message on any failure.
func (o *Ops) GenerateTitle(threadID string, assistantMessage string, gen TitleGenerator) (string, error) {
	t, err := o.store.GetThread(threadID)
	if err != nil {
		return "", err
	}
	if t == nil {
		return "", ErrThreadNotFound(threadID)
	}

	// Never overwrite a user-renamed title.
	if !IsAutoGeneratedTitle(t.Title) && t.Title != DefaultThreadTitle() {
		return t.Title, nil
	}

	msgs, err := o.store.GetMessages(threadID, 200)
	if err != nil {
		return "", err
	}

	// Find first user message.
	var userMsg string
	for _, m := range msgs {
		if m.Role == "user" && m.Content != "" {
			userMsg = m.Content
			break
		}
	}

	// Find first assistant message if not provided.
	assistant := assistantMessage
	if assistant == "" {
		for _, m := range msgs {
			if m.Role == "assistant" && m.Content != "" {
				assistant = m.Content
				break
			}
		}
	}

	// If no assistant message, use deterministic fallback.
	if assistant == "" {
		title := FallbackTitleFromMessage(userMsg)
		if title == "" {
			title = DefaultThreadTitle()
		}
		o.store.UpdateThreadTitle(threadID, title)
		return title, nil
	}

	// Try LLM generation.
	if gen != nil && userMsg != "" {
		if llmTitle, err := gen.GenerateTitle(userMsg, assistant); err == nil && llmTitle != "" {
			llmTitle = SanitizeTitle(llmTitle)
			if llmTitle != "" {
				o.store.UpdateThreadTitle(threadID, llmTitle)
				return llmTitle, nil
			}
		}
	}

	// Fallback.
	title := FallbackTitleFromMessage(userMsg)
	if title == "" {
		title = DefaultThreadTitle()
	}
	o.store.UpdateThreadTitle(threadID, title)
	return title, nil
}

// ErrThreadNotFound returns a typed not-found error.
func ErrThreadNotFound(threadID string) error {
	return &ThreadNotFoundError{ThreadID: threadID}
}

// ThreadNotFoundError is a typed error for missing threads.
type ThreadNotFoundError struct {
	ThreadID string
}

func (e *ThreadNotFoundError) Error() string {
	return fmt.Sprintf("thread %s not found", e.ThreadID)
}

func (e *ThreadNotFoundError) Kind() string {
	return "ThreadNotFound"
}
