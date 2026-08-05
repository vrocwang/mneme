// Package integration defines standard interfaces for connecting external services
// (OAuth providers, data sync connectors) into Mneme. Concrete implementations
// live in separate extension packages and register at runtime.
package integration

import (
	"context"
	"time"
)

// ── OAuth provider ───────────────────────────────────────────────────────────

// OAuthToken holds an OAuth 2.0 token set (access + optional refresh).
type OAuthToken struct {
	AccessToken  string                 `json:"access_token"`
	RefreshToken string                 `json:"refresh_token,omitempty"`
	TokenType    string                 `json:"token_type,omitempty"`
	Scope        string                 `json:"scope,omitempty"`
	ExpiresAt    time.Time              `json:"expires_at,omitempty"`
	Raw          map[string]interface{} `json:"raw,omitempty"` // provider-specific extras
}

// IsExpired returns true when the token has passed its expiry with 60s buffer.
func (t *OAuthToken) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(60 * time.Second).After(t.ExpiresAt)
}

// OAuthProvider describes an OAuth 2.0 integration that can be authorized,
// refreshed, and revoked.
type OAuthProvider interface {
	// ID returns a stable provider identifier (e.g. "google", "github", "notion").
	ID() string
	// Name returns a human-readable provider name.
	Name() string
	// AuthURL builds the OAuth authorization URL for the given state parameter.
	AuthURL(state string) (string, error)
	// ExchangeCode exchanges an authorization code for a token set.
	ExchangeCode(ctx context.Context, code string) (*OAuthToken, error)
	// RefreshToken obtains a new access token using a refresh token.
	RefreshToken(ctx context.Context, token *OAuthToken) (*OAuthToken, error)
	// RevokeToken revokes the given token (best-effort).
	RevokeToken(ctx context.Context, token *OAuthToken) error
}

// ── Sync connector ───────────────────────────────────────────────────────────

// SyncDocument is a single document produced by an external data source sync.
type SyncDocument struct {
	Source   string            `json:"source"` // connector ID
	Path     string            `json:"path"`   // logical path within the source
	Title    string            `json:"title"`
	Content  string            `json:"content"`
	URL      string            `json:"url,omitempty"`
	Modified time.Time         `json:"modified"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// SyncStatus reports the current state of a sync connector.
type SyncStatus struct {
	Connected    bool      `json:"connected"`
	LastSyncAt   time.Time `json:"last_sync_at,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	DocsIngested int64     `json:"docs_ingested"`
}

// SyncConnector represents an external data source that can be synced into
// the memory pipeline. It requires authentication via an OAuthToken.
type SyncConnector interface {
	// ID returns a stable connector identifier (e.g. "gmail-sync").
	ID() string
	// Kind returns the data source kind for categorization ("gmail", "slack", "github", ...).
	Kind() string
	// Name returns a human-readable connector name.
	Name() string
	// Connect establishes the connection using the given OAuth token.
	Connect(ctx context.Context, token *OAuthToken) error
	// Disconnect tears down the connection.
	Disconnect(ctx context.Context) error
	// Sync performs one synchronization pass.
	Sync(ctx context.Context) ([]SyncDocument, error)
	// Status returns current connection and sync status.
	Status() SyncStatus
}

// ── Provider / connector descriptors ─────────────────────────────────────────

// ProviderDescriptor summarises a registered OAuthProvider for discovery.
type ProviderDescriptor struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	AuthURL string `json:"auth_url,omitempty"` // pre-built with empty state for preview
	IconURL string `json:"icon_url,omitempty"`
}

// ConnectorDescriptor summarises a registered SyncConnector for discovery.
type ConnectorDescriptor struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Name      string     `json:"name"`
	Connected bool       `json:"connected"`
	Status    SyncStatus `json:"status"`
}

// ── Registry ────────────────────────────────────────────────────────────────

// IntegrationRegistry is the central registry for OAuth providers and
// sync connectors. Providers and connectors register at startup and are
// discoverable by the UI and agent tools.
type IntegrationRegistry interface {
	// OAuth providers.
	RegisterOAuthProvider(p OAuthProvider) error
	UnregisterOAuthProvider(id string) error
	ListProviders() []ProviderDescriptor
	GetProvider(id string) (OAuthProvider, error)

	// Sync connectors.
	RegisterSyncConnector(c SyncConnector) error
	UnregisterSyncConnector(id string) error
	ListConnectors() []ConnectorDescriptor
	GetConnector(id string) (SyncConnector, error)
}
