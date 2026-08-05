// Package client provides an MCP JSON-RPC client with optional authentication
// via the AuthProvider extension point. Implementations supply Bearer, Basic,
// Header, QueryParam, and OAuth credentials on demand.
package client

import (
	"context"
	"fmt"
	"time"
)

// AuthHints describes the authentication methods the MCP server advertises.
// Servers signal hints via WWW-Authenticate headers or the MCP handshake.
type AuthHints struct {
	Scheme     string   // "Bearer", "Basic", "OAuth", "Header", "QueryParam"
	Scopes     []string // OAuth scopes requested by the server
	Realm      string   // auth realm (Basic)
	HeaderName string   // custom header name (Header scheme)
	ParamName  string   // query parameter name (QueryParam scheme)

	// OAuth endpoint URLs (populated from server metadata or discovery).
	AuthURL  string // authorization endpoint URL
	TokenURL string // token exchange endpoint URL
}

// AuthResult carries the credentials obtained after authentication.
type AuthResult struct {
	// Bearer / OAuth
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`

	// Basic / Header / QueryParam
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	HeaderName  string `json:"header_name,omitempty"`
	HeaderValue string `json:"header_value,omitempty"`
	QueryParam  string `json:"query_param,omitempty"`
	QueryValue  string `json:"query_value,omitempty"`
}

// AuthProvider authenticates an MCP client connection. Implementations may
// prompt the user, consult a keychain, or run an OAuth flow.
type AuthProvider interface {
	// Authenticate obtains credentials for connecting to an MCP server. The
	// hints parameter describes the auth schemes the server supports. If the
	// server requires no authentication, return (AuthResult{}, nil).
	Authenticate(ctx context.Context, serverURL string, hints AuthHints) (AuthResult, error)

	// Refresh obtains a new access token using a refresh token. Return the
	// updated AuthResult. Not all providers support refresh — callers should
	// fall back to re-authenticating on failure.
	Refresh(ctx context.Context, serverURL string, refreshToken string) (AuthResult, error)
}

// NoAuthProvider is an AuthProvider that always returns empty credentials.
// Use for public MCP servers that don't require authentication.
type NoAuthProvider struct{}

func (NoAuthProvider) Authenticate(_ context.Context, _ string, _ AuthHints) (AuthResult, error) {
	return AuthResult{}, nil
}
func (NoAuthProvider) Refresh(_ context.Context, _ string, _ string) (AuthResult, error) {
	return AuthResult{}, fmt.Errorf("NoAuthProvider does not support refresh")
}
