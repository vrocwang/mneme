package threads

import "time"

// ── Request / response types ──────────────────────────────────────

type CreateThreadRequest struct {
	Title         string   `json:"title,omitempty"`
	Labels        []string `json:"labels,omitempty"`
	PersonalityID string   `json:"personalityId,omitempty"`
}

type UpdateThreadRequest struct {
	ThreadID      string   `json:"threadId"`
	Title         string   `json:"title,omitempty"`
	Labels        []string `json:"labels,omitempty"`
	PersonalityID string   `json:"personalityId,omitempty"`
}

type DeleteThreadRequest struct {
	ThreadID string `json:"threadId"`
}

type GenerateTitleRequest struct {
	ThreadID         string `json:"threadId"`
	AssistantMessage string `json:"assistantMessage,omitempty"`
}

type MessagesRequest struct {
	ThreadID string `json:"threadId"`
	Limit    int    `json:"limit,omitempty"`
	AfterID  int64  `json:"afterId,omitempty"`
}

type AppendMessageRequest struct {
	ThreadID string            `json:"threadId"`
	Role     string            `json:"role"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type UpdateMessageRequest struct {
	MessageID int64             `json:"messageId"`
	Content   string            `json:"content,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type ThreadSummary struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Labels        []string `json:"labels,omitempty"`
	PersonalityID string   `json:"personalityId,omitempty"`
	MessageCount  int      `json:"message_count"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

// ── Turn state types ──────────────────────────────────────────────

type TurnLifecycle string

const (
	TurnStarted     TurnLifecycle = "started"
	TurnStreaming   TurnLifecycle = "streaming"
	TurnInterrupted TurnLifecycle = "interrupted"
)

type TurnPhase string

const (
	PhaseThinking TurnPhase = "thinking"
	PhaseToolUse  TurnPhase = "tool_use"
	PhaseSubagent TurnPhase = "subagent"
)

type ToolTimelineEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Round  int    `json:"round"`
	Status string `json:"status"` // running, success, error
	Args   string `json:"argsBuffer,omitempty"`
	Error  string `json:"error,omitempty"`
}

type TurnState struct {
	ThreadID      string              `json:"threadId"`
	RequestID     string              `json:"requestId"`
	Lifecycle     TurnLifecycle       `json:"lifecycle"`
	Iteration     int                 `json:"iteration"`
	MaxIterations int                 `json:"maxIterations"`
	Phase         TurnPhase           `json:"phase,omitempty"`
	ActiveTool    string              `json:"activeTool,omitempty"`
	StreamingText string              `json:"streamingText,omitempty"`
	Thinking      string              `json:"thinking,omitempty"`
	ToolTimeline  []ToolTimelineEntry `json:"toolTimeline,omitempty"`
	StartedAt     string              `json:"startedAt"`
	UpdatedAt     string              `json:"updatedAt"`
}

func NewTurnState(threadID, requestID string, maxIterations int) *TurnState {
	now := time.Now().UTC().Format(time.RFC3339)
	return &TurnState{
		ThreadID:      threadID,
		RequestID:     requestID,
		Lifecycle:     TurnStarted,
		MaxIterations: maxIterations,
		StartedAt:     now,
		UpdatedAt:     now,
	}
}

// ── Wails-facing result types ─────────────────────────────────────

type ThreadListResult struct {
	Threads []ThreadSummary `json:"threads"`
}

type ThreadResult struct {
	Thread ThreadSummary `json:"thread"`
}

type MessagesResult struct {
	ThreadID string          `json:"threadId"`
	Messages []MessageRecord `json:"messages"`
}

type MessageRecord struct {
	ID        int64             `json:"id"`
	ThreadID  string            `json:"threadId"`
	Role      string            `json:"role"`
	Content   string            `json:"content"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt string            `json:"created_at"`
}

type DeleteResult struct {
	ThreadID string `json:"threadId"`
	Deleted  bool   `json:"deleted"`
}

type PurgeResult struct {
	DeletedCount int64 `json:"deletedCount"`
}

type ErrorResult struct {
	Error string `json:"error"`
	Kind  string `json:"kind,omitempty"`
}
