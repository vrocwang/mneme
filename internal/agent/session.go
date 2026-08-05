package agent

import (
	"sync"

	"github.com/simon/mneme/internal/inference"
)

// MessagePersister is the interface for persisting messages to storage.
type MessagePersister interface {
	AppendMessage(threadID string, msg inference.Message) error
}

// Session holds in-memory message history for a thread.
type Session struct {
	mu       sync.RWMutex
	ThreadID string
	messages []inference.Message

	// persister is an optional hook that writes each Append to durable storage.
	persister MessagePersister

	// frozenPrefix stores the message snapshot at the end of the previous
	// turn. On the next turn these messages can be reused as a KV-cache
	// prefix to avoid re-processing them (saves token costs).
	frozenPrefix       []inference.Message
	frozenSystemPrompt string
}

// FreezePrefix captures the current messages as a frozen KV-cache prefix.
// Call at the end of each turn to enable transcript resume on the next turn.
func (s *Session) FreezePrefix() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frozenPrefix = make([]inference.Message, len(s.messages))
	copy(s.frozenPrefix, s.messages)
}

// HasFrozenPrefix returns true when a frozen prefix from a previous turn
// is available for KV-cache reuse.
func (s *Session) HasFrozenPrefix() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.frozenPrefix) > 0
}

// FreezeSystemPrompt stores the built system prompt for reuse on subsequent turns.
func (s *Session) FreezeSystemPrompt(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frozenSystemPrompt = prompt
}

// FrozenSystemPrompt returns the cached system prompt, or empty if not yet frozen.
func (s *Session) FrozenSystemPrompt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.frozenSystemPrompt
}

// HasFrozenSystemPrompt returns true when a system prompt has been frozen.
func (s *Session) HasFrozenSystemPrompt() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.frozenSystemPrompt != ""
}

// FrozenPrefix returns the frozen prefix messages for KV-cache reuse.
// Returns nil when no prefix is available.
func (s *Session) FrozenPrefix() []inference.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.frozenPrefix) == 0 {
		return nil
	}
	result := make([]inference.Message, len(s.frozenPrefix))
	copy(result, s.frozenPrefix)
	return result
}

// NewSession creates a session with no persistence hook.
func NewSession(threadID string) *Session {
	return &Session{ThreadID: threadID, messages: make([]inference.Message, 0)}
}

// NewPersistentSession creates a session that persists every Append to the given store.
func NewPersistentSession(threadID string, p MessagePersister) *Session {
	return &Session{ThreadID: threadID, messages: make([]inference.Message, 0), persister: p}
}

// Append adds a message to the session. If a persister is configured the
// message is written to durable storage after releasing the write lock so
// that I/O never blocks concurrent readers.
func (s *Session) Append(msg inference.Message) {
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	p := s.persister
	s.mu.Unlock()

	if p != nil {
		// best-effort persistence — errors are logged by the persister
		p.AppendMessage(s.ThreadID, msg)
	}
}

// History returns the last `max` messages.
func (s *Session) History(max int) []inference.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.messages) <= max {
		result := make([]inference.Message, len(s.messages))
		copy(result, s.messages)
		return result
	}
	start := len(s.messages) - max
	result := make([]inference.Message, max)
	copy(result, s.messages[start:])
	return result
}

// Hydrate loads messages from the given records into the session (used at startup).
func (s *Session) Hydrate(records []PersistedMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range records {
		msg := inference.Message{
			Role:    r.Role,
			Content: r.Content,
			ToolID:  r.ToolID,
		}
		if r.ToolCallID != "" {
			msg.ToolCall = &inference.ToolCall{
				ID:        r.ToolCallID,
				Name:      r.ToolCallName,
				Arguments: r.ToolCallArgs,
			}
		}
		s.messages = append(s.messages, msg)
	}
}

// MsgCount returns the number of messages currently in the session.
func (s *Session) MsgCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages)
}

// TruncateTo truncates the session to keep only the first n messages.
// This is used to roll back partial tool-call results on abort so that
// incomplete assistant/tool-result pairs do not leak into the next turn.
func (s *Session) TruncateTo(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 0 {
		n = 0
	}
	if n < len(s.messages) {
		s.messages = s.messages[:n]
	}
}

// PersistedMessage is a message record from durable storage.
type PersistedMessage struct {
	Role         string
	Content      string
	ToolID       string // tool result correlation ID
	ToolCallID   string // native tool call id
	ToolCallName string // native tool call function name
	ToolCallArgs []byte // native tool call JSON arguments
}
