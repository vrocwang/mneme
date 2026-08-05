package integration

import (
	"context"
	"encoding/json"
	"time"
)

// Action describes a single executable action on a connected service.
type Action struct {
	ID          string                 `json:"id"`   // e.g. "GMAIL_SEND_EMAIL"
	Name        string                 `json:"name"` // e.g. "Send Email"
	Description string                 `json:"description"`
	Toolkit     string                 `json:"toolkit"`    // e.g. "gmail"
	Parameters  map[string]interface{} `json:"parameters"` // JSON Schema
	Scope       string                 `json:"scope"`      // "read" or "write"
}

// ActionResult holds the result of executing an action.
type ActionResult struct {
	Success           bool            `json:"success"`
	Data              json.RawMessage `json:"data,omitempty"`
	Error             string          `json:"error,omitempty"`
	MarkdownFormatted string          `json:"markdown_formatted,omitempty"`
	Elapsed           time.Duration   `json:"elapsed_ms"`
}

// ActionExecutor executes actions on connected third-party services
// (e.g. sending email via Gmail, creating issues via GitHub).
// Implementations handle authentication, request construction, and
// error mapping for a specific integration backend (Composio, direct API, etc.).
type ActionExecutor interface {
	// ID returns the executor identifier (e.g. "composio", "github-direct").
	ID() string

	// ListActions returns all available actions, optionally filtered by toolkit.
	ListActions(ctx context.Context, connectionID string, toolkit string) ([]Action, error)

	// Execute runs an action by its ID with the given parameters.
	Execute(ctx context.Context, connectionID, actionID string, params map[string]interface{}) (*ActionResult, error)

	// SupportedToolkits returns the toolkit slugs this executor knows about.
	SupportedToolkits() []string
}

// ToolkitDescriptor summarises an available integration toolkit for UI discovery.
type ToolkitDescriptor struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ActionCount int    `json:"action_count"`
	ScopeRead   bool   `json:"scope_read"`
	ScopeWrite  bool   `json:"scope_write"`
}
