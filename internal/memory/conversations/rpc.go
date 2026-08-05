package conversations

import (
	"log/slog"

	"github.com/google/uuid"
)

// ThreadsRPC provides Wails-bound methods for thread management.
// The store is accessed via a getter function so it can be initialized
// after Bind time (Wails binds are created before OnStartup).
type ThreadsRPC struct {
	storeFn func() *Store
	log     *slog.Logger
}

// NewThreadsRPC creates a thread management RPC handler.
func NewThreadsRPC(storeFn func() *Store) *ThreadsRPC {
	return &ThreadsRPC{storeFn: storeFn, log: slog.Default().With("component", "ThreadsRPC")}
}

func (r *ThreadsRPC) store() *Store {
	if r.storeFn == nil {
		r.log.Warn("store getter is nil, thread operations will fail")
		return nil
	}
	s := r.storeFn()
	if s == nil {
		r.log.Warn("store is nil, thread operations will fail — check startup logs for 'open database failed' or 'conversations store init failed'")
	}
	return s
}

// ListThreads returns all conversation threads.
func (r *ThreadsRPC) ListThreads() []map[string]interface{} {
	s := r.store()
	if s == nil {
		return []map[string]interface{}{}
	}
	threads, err := s.ListThreads(200)
	if err != nil {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(threads))
	for _, t := range threads {
		count, _ := s.CountMessages(t.ID)
		out = append(out, map[string]interface{}{
			"id":            t.ID,
			"title":         t.Title,
			"created_at":    t.CreatedAt,
			"updated_at":    t.UpdatedAt,
			"message_count": count,
		})
	}
	return out
}

// CreateThread creates a new conversation thread.
func (r *ThreadsRPC) CreateThread(title string) map[string]interface{} {
	s := r.store()
	if s == nil {
		return map[string]interface{}{"error": "store not available"}
	}
	id := uuid.New().String()
	if title == "" {
		title = "New conversation"
	}
	if err := s.CreateThread(id, title, ""); err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return map[string]interface{}{
		"id": id, "title": title,
		"created_at": "", "updated_at": "", "message_count": 0,
	}
}

// DeleteThread removes a thread and its messages.
func (r *ThreadsRPC) DeleteThread(threadID string) {
	s := r.store()
	if s != nil {
		s.DeleteThread(threadID)
	}
}

// GetThreadMessages returns messages for a thread.
func (r *ThreadsRPC) GetThreadMessages(threadID string, limit int, afterID int) []map[string]interface{} {
	s := r.store()
	if s == nil {
		return []map[string]interface{}{}
	}
	msgs, err := s.GetMessages(threadID, limit)
	if err != nil {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(msgs))
	for _, m := range msgs {
		if afterID > 0 && int(m.ID) <= afterID {
			continue
		}
		out = append(out, map[string]interface{}{
			"id":         m.ID,
			"role":       m.Role,
			"content":    m.Content,
			"created_at": m.CreatedAt,
		})
	}
	return out
}

// UpdateThreadTitle updates a thread's title.
func (r *ThreadsRPC) UpdateThreadTitle(threadID, title string) {
	s := r.store()
	if s != nil {
		s.UpdateThreadTitle(threadID, title)
	}
}
