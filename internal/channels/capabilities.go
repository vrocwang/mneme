package channels

import "context"

// TypingSender is an optional interface for channels that support
// typing indicators (e.g. Discord, Slack, Telegram).
type TypingSender interface {
	StartTyping(ctx context.Context, target string) error
	StopTyping(ctx context.Context, target string) error
}

// HealthChecker is an optional interface for channels that can
// report their connection health.
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// ReactionSender is an optional interface for channels that support
// emoji reactions on messages.
type ReactionSender interface {
	SupportsReactions() bool
	SendReaction(ctx context.Context, messageID, emoji string) error
}

// DraftSender is an optional interface for channels that support
// draft message updates (e.g. streaming response previews).
type DraftSender interface {
	SupportsDraftUpdates() bool
	SendDraft(ctx context.Context, target, content string) error
	UpdateDraft(ctx context.Context, draftID, content string) error
	FinalizeDraft(ctx context.Context, draftID string) error
}

// ThreadCreator is an optional interface for channels that support
// threaded conversations.
type ThreadCreator interface {
	CreateThread(ctx context.Context, parentID, title string) (string, error)
	ListThreads(ctx context.Context, limit int) ([]ThreadInfo, error)
	UpdateThread(ctx context.Context, threadID string, title string) error
}

// ThreadInfo carries metadata about a conversation thread.
type ThreadInfo struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	MessageCount int    `json:"message_count"`
	IsArchived   bool   `json:"is_archived"`
}

// ── Channel Definition (UI-facing metadata) ─────────────────────────

// AuthModeSpec describes how this channel authenticates.
type AuthModeSpec struct {
	Type        string   `json:"type"` // "oauth", "api_key", "bot_token", "webhook", "none"
	RedirectURI string   `json:"redirect_uri,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
}

// ChannelCapability lists optional features a channel provider supports.
type ChannelCapability string

const (
	CapTyping    ChannelCapability = "typing"
	CapReactions ChannelCapability = "reactions"
	CapDrafts    ChannelCapability = "drafts"
	CapThreads   ChannelCapability = "threads"
	CapHealth    ChannelCapability = "health"
)

// FieldRequirement describes a configuration field for the channel setup form.
type FieldRequirement struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // "text", "password", "select", "boolean"
	Required    bool     `json:"required"`
	Placeholder string   `json:"placeholder,omitempty"`
	Default     string   `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// ChannelDefinition provides UI-facing metadata for channel setup forms.
type ChannelDefinition struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	AuthModes    []AuthModeSpec      `json:"auth_modes"`
	Capabilities []ChannelCapability `json:"capabilities"`
	Fields       []FieldRequirement  `json:"fields"`
	IconURL      string              `json:"icon_url,omitempty"`
}

// DefinableChannel is an optional interface for channels that provide
// UI-facing metadata for the setup form.
type DefinableChannel interface {
	Definition() ChannelDefinition
}

// ── Channel Controller Operations ───────────────────────────────────

// ConnectionStatus represents the current state of a channel connection.
type ConnectionStatus struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Healthy   bool   `json:"healthy"`
	Error     string `json:"error,omitempty"`
	LastEvent string `json:"last_event,omitempty"`
}
