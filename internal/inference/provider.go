package inference

import (
	"context"
	"encoding/json"
)

// ── Message types ─────────────────────────────────────────────────────────

// ContentBlock represents a single content item in a multimodal message.
type ContentBlock struct {
	Type      string `json:"type"`                 // "text" or "image"
	Text      string `json:"text,omitempty"`       // for text blocks
	ImageURL  string `json:"image_url,omitempty"`  // for image blocks (URL)
	ImageData string `json:"image_data,omitempty"` // for image blocks (base64)
	ImageType string `json:"image_type,omitempty"` // "image/png", "image/jpeg", etc.
}

type Message struct {
	Role          string         `json:"role"`
	Content       string         `json:"content,omitempty"`        // plain text (backward-compatible)
	ContentBlocks []ContentBlock `json:"content_blocks,omitempty"` // multimodal content
	Thinking      string         `json:"thinking,omitempty"`       // extended thinking content
	Signature     string         `json:"signature,omitempty"`      // thinking signature for verification
	ToolCall      *ToolCall      `json:"tool_call,omitempty"`
	ToolID        string         `json:"tool_id,omitempty"`
}

// HasMultimodal returns true if the message contains image content blocks.
func (m *Message) HasMultimodal() bool {
	for _, b := range m.ContentBlocks {
		if b.Type == "image" {
			return true
		}
	}
	return false
}

// TextContent returns the plain-text representation of the message content.
func (m *Message) TextContent() string {
	if m.Content != "" {
		return m.Content
	}
	for _, b := range m.ContentBlocks {
		if b.Type == "text" {
			return b.Text
		}
	}
	return ""
}

// ToolCall is a structured tool invocation from the provider (native tool calling).
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolDefinition describes a tool to the provider for native tool calling.
type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"` // JSON Schema object
}

// ── Streaming ─────────────────────────────────────────────────────────────

// Usage captures token counts from a provider response for cost tracking.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CacheTokens  int `json:"cache_tokens,omitempty"` // cached/prompt-cache tokens
}

// Token represents a streaming token from a provider.
type Token struct {
	Text      string    `json:"text,omitempty"`
	Thinking  string    `json:"thinking,omitempty"`  // reasoning/thinking content (Claude extended thinking)
	Signature string    `json:"signature,omitempty"` // thinking signature for verification
	IsFinal   bool      `json:"is_final"`
	ToolCall  *ToolCall `json:"tool_call,omitempty"` // non-nil when a tool call is being streamed
	Usage     *Usage    `json:"usage,omitempty"`     // non-nil on the final token when provider reports usage
}

// ── Chat request ───────────────────────────────────────────────────────────

type ChatRequest struct {
	Model        string
	Messages     []Message
	Tools        []ToolDefinition // optional — enables native tool calling when non-empty
	SystemPrompt string           // optional system prompt (Anthropic sends separately from messages)
	MaxTokens    int              // 0 means use provider default
	Temperature  float64          // 0 means use provider default
}

// ── Provider interface ─────────────────────────────────────────────────────

// LifecycleManager is an optional interface for providers that manage their own
// runtime (start, stop, health check, model pull). Local inference services
// implement this via extensions.
type LifecycleManager interface {
	Start(ctx context.Context) error
	Stop() error
	HealthCheck(ctx context.Context) error
	PullModel(ctx context.Context, model string) error
	ListModels(ctx context.Context) ([]string, error)
}

type Provider interface {
	Name() string
	Chat(ctx context.Context, req ChatRequest) (<-chan Token, <-chan error)
}

// VisionProvider is an optional interface for providers that can report
// whether they support vision/image inputs.
type VisionProvider interface {
	SupportsVision() bool
}

// ── MockProvider for testing ───────────────────────────────────────────────

type MockProvider struct {
	NameStr string
	Tokens  []Token
}

func (m *MockProvider) Name() string {
	if m.NameStr == "" {
		return "mock"
	}
	return m.NameStr
}

func (m *MockProvider) Chat(ctx context.Context, req ChatRequest) (<-chan Token, <-chan error) {
	tokens := make(chan Token, len(m.Tokens))
	errs := make(chan error, 1)
	go func() {
		defer close(tokens)
		defer close(errs)
		for _, tok := range m.Tokens {
			select {
			case tokens <- tok:
			case <-ctx.Done():
				return
			}
		}
	}()
	return tokens, errs
}

// ── Helpers ────────────────────────────────────────────────────────────────

// IsToolCall checks if a message contains a native tool call (vs text content).
func (m *Message) IsToolCall() bool {
	return m.ToolCall != nil
}

// IsToolResult checks if a message is a tool result (role == "tool").
func (m *Message) IsToolResult() bool {
	return m.Role == "tool"
}
