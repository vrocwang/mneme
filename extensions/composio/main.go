// Composio extension for Mneme.
//
// Provides OAuth SaaS integration tools via the Composio backend API.
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
//
// Configuration: set COMPOSIO_BACKEND_URL and COMPOSIO_API_KEY environment
// variables, or call extension.configure with {"backend_url":"...","api_key":"..."}.
//
// Tools:
//   - composio_list_toolkits: list available integration toolkits
//   - composio_execute_action: execute an action on a connected service
//   - composio_list_connections: list active OAuth connections
//   - composio_start_oauth: initiate OAuth handoff (returns auth_url)
//   - composio_complete_oauth: exchange code for connection
//   - composio_refresh_token: refresh an OAuth access token
//   - composio_revoke_token: revoke an OAuth connection
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

// ── Composio API client (stdlib only, no core deps) ──────────────────────

type composioClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func newClient(backendURL, apiKey string) *composioClient {
	return &composioClient{
		baseURL: strings.TrimRight(backendURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *composioClient) do(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal: %w", err)
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("composio API error %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

// ── OAuth helpers ─────────────────────────────────────────────────────────

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// stateStorePath returns the path to the persistent OAuth state file.
func stateStorePath() string {
	cacheDir := os.Getenv("MNEME_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "mneme-composio")
	}
	os.MkdirAll(cacheDir, 0700)
	return filepath.Join(cacheDir, "oauth_states.json")
}

func loadOAuthStates() (map[string]string, error) {
	states := map[string]string{}
	data, err := os.ReadFile(stateStorePath())
	if err != nil {
		if os.IsNotExist(err) {
			return states, nil
		}
		return nil, err
	}
	json.Unmarshal(data, &states)
	return states, nil
}

func saveOAuthStates(states map[string]string) error {
	data, err := json.Marshal(states)
	if err != nil {
		return err
	}
	return os.WriteFile(stateStorePath(), data, 0600)
}

// ── Global state ──────────────────────────────────────────────────────────

var (
	client      *composioClient
	oauthStates = map[string]string{} // state → app name
)

// ── Main ─────────────────────────────────────────────────────────────────

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("composio extension starting")

	// Check for env-var configuration.
	if url := os.Getenv("COMPOSIO_BACKEND_URL"); url != "" {
		if key := os.Getenv("COMPOSIO_API_KEY"); key != "" {
			client = newClient(url, key)
			log.Info("composio configured via environment")
		}
	}

	// Restore OAuth states from disk so in-flight handoffs survive restarts.
	if loaded, err := loadOAuthStates(); err == nil {
		oauthStates = loaded
		log.Info("oauth states restored", "count", len(oauthStates))
	}

	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "composio",
		Version:     "0.1.0",
		Description: "Composio OAuth SaaS integrations",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "composio_list_toolkits",
		Description: "List available Composio integration toolkits (Gmail, Slack, GitHub, Notion, etc.)",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, listToolkits)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "composio_execute_action",
		Description: "Execute an action on a connected Composio integration. Requires the toolkit name, action name, and action-specific parameters.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"toolkit": map[string]interface{}{"type": "string", "description": "The toolkit/app name (e.g. github, gmail, slack)"},
				"action":  map[string]interface{}{"type": "string", "description": "The action to execute on the toolkit"},
				"params":  map[string]interface{}{"type": "object", "description": "Action-specific parameters"},
			},
			"required": []string{"toolkit", "action"},
		},
		Permission: "execute",
		HasEffects: true,
	}, executeAction)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "composio_list_connections",
		Description: "List active OAuth connections for the current user",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, listConnections)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "composio_start_oauth",
		Description: "Start OAuth handoff for a service. Returns an authorization URL the user must visit.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"app":          map[string]interface{}{"type": "string", "description": "The app/service to connect (e.g. github, gmail, slack)"},
				"redirect_uri": map[string]interface{}{"type": "string", "description": "OAuth redirect URI"},
			},
			"required": []string{"app"},
		},
		Permission: "execute",
		HasEffects: true,
	}, startOAuth)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "composio_complete_oauth",
		Description: "Complete OAuth handoff by exchanging the authorization code for a connection",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"state": map[string]interface{}{"type": "string", "description": "OAuth state token from start_oauth"},
				"code":  map[string]interface{}{"type": "string", "description": "Authorization code from OAuth redirect"},
			},
			"required": []string{"state", "code"},
		},
		Permission: "execute",
		HasEffects: true,
	}, completeOAuth)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "composio_refresh_token",
		Description: "Refresh an expired OAuth access token for a connected service",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connection_id": map[string]interface{}{"type": "string", "description": "The connection ID to refresh"},
			},
			"required": []string{"connection_id"},
		},
		Permission: "execute",
		HasEffects: true,
	}, refreshToken)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "composio_revoke_token",
		Description: "Revoke an OAuth connection and delete its tokens",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connection_id": map[string]interface{}{"type": "string", "description": "The connection ID to revoke"},
			},
			"required": []string{"connection_id"},
		},
		Permission: "execute",
		HasEffects: true,
	}, revokeToken)

	srv.SetConfigHandler(func(cfg map[string]interface{}) error {
		url, _ := cfg["backend_url"].(string)
		key, _ := cfg["api_key"].(string)
		if url == "" || key == "" {
			return fmt.Errorf("backend_url and api_key required")
		}
		client = newClient(url, key)
		return nil
	})

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "composio: %v\n", err)
		os.Exit(1)
	}
}

// ── Tool implementations ──────────────────────────────────────────────────

func listToolkits(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = args
	if client == nil {
		return extsdk.Result{Error: "composio not configured. Set COMPOSIO_BACKEND_URL and COMPOSIO_API_KEY."}
	}
	body, err := client.do(ctx, "GET", "/api/composio/toolkits", nil)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("list toolkits: %v", err)}
	}
	var resp struct {
		Toolkits []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			AppName     string `json:"app_name"`
			Enabled     bool   `json:"enabled"`
		} `json:"toolkits"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("parse: %v", err)}
	}
	b, _ := json.MarshalIndent(resp.Toolkits, "", "  ")
	return extsdk.Result{Success: true, Output: string(b)}
}

func executeAction(ctx context.Context, args map[string]interface{}) extsdk.Result {
	if client == nil {
		return extsdk.Result{Error: "composio not configured"}
	}
	toolkit, _ := args["toolkit"].(string)
	action, _ := args["action"].(string)
	params, _ := args["params"].(map[string]interface{})
	if toolkit == "" || action == "" {
		return extsdk.Result{Error: "toolkit and action required"}
	}
	if params == nil {
		params = map[string]interface{}{}
	}
	req := map[string]interface{}{"toolkit": toolkit, "action": action, "params": params}
	body, err := client.do(ctx, "POST", "/api/composio/actions/execute", req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("execute: %v", err)}
	}
	var resp struct {
		Success bool   `json:"success"`
		Output  string `json:"output"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("parse response: %v", err)}
	}
	return extsdk.Result{Success: resp.Success, Output: resp.Output, Error: resp.Error}
}

func listConnections(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = args
	if client == nil {
		return extsdk.Result{Error: "composio not configured"}
	}
	body, err := client.do(ctx, "GET", "/api/composio/connections", nil)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("list connections: %v", err)}
	}
	var resp struct {
		Connections []struct {
			ID        string `json:"id"`
			AppName   string `json:"app_name"`
			Status    string `json:"status"`
			CreatedAt string `json:"created_at"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("parse response: %v", err)}
	}
	b, _ := json.MarshalIndent(resp.Connections, "", "  ")
	return extsdk.Result{Success: true, Output: string(b)}
}

func startOAuth(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = ctx
	if client == nil {
		return extsdk.Result{Error: "composio not configured"}
	}
	app, _ := args["app"].(string)
	redirectURI, _ := args["redirect_uri"].(string)
	if app == "" {
		return extsdk.Result{Error: "app is required"}
	}
	state, err := generateState()
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("generate state: %v", err)}
	}
	oauthStates[state] = app
	_ = saveOAuthStates(oauthStates) // best-effort persist for crash recovery

	body, err := client.do(context.Background(), "POST", "/api/composio/oauth/start", map[string]string{
		"app": app, "redirect_uri": redirectURI, "state": state,
	})
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("start oauth: %v", err)}
	}
	var resp struct {
		AuthURL string `json:"auth_url"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("parse response: %v", err)}
	}
	b, _ := json.Marshal(map[string]string{"auth_url": resp.AuthURL, "state": state})
	return extsdk.Result{Success: true, Output: string(b)}
}

func completeOAuth(ctx context.Context, args map[string]interface{}) extsdk.Result {
	if client == nil {
		return extsdk.Result{Error: "composio not configured"}
	}
	state, _ := args["state"].(string)
	code, _ := args["code"].(string)
	if state == "" || code == "" {
		return extsdk.Result{Error: "state and code required"}
	}
	body, err := client.do(ctx, "POST", "/api/composio/oauth/complete", map[string]string{
		"state": state, "code": code,
	})
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("complete oauth: %v", err)}
	}

	// Clean up the used state from memory and disk.
	delete(oauthStates, state)
	_ = saveOAuthStates(oauthStates)

	var resp struct {
		ConnectionID string `json:"connection_id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("parse response: %v", err)}
	}
	b, _ := json.Marshal(map[string]string{"connection_id": resp.ConnectionID})
	return extsdk.Result{Success: true, Output: string(b)}
}

func refreshToken(ctx context.Context, args map[string]interface{}) extsdk.Result {
	if client == nil {
		return extsdk.Result{Error: "composio not configured"}
	}
	connectionID, _ := args["connection_id"].(string)
	if connectionID == "" {
		return extsdk.Result{Error: "connection_id is required"}
	}
	body, err := client.do(ctx, "POST", "/api/composio/oauth/refresh", map[string]string{
		"connection_id": connectionID,
	})
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("refresh token: %v", err)}
	}
	var resp struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   string `json:"expires_at,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("parse response: %v", err)}
	}
	b, _ := json.Marshal(map[string]string{
		"access_token": resp.AccessToken,
		"expires_at":   resp.ExpiresAt,
	})
	return extsdk.Result{Success: true, Output: string(b)}
}

func revokeToken(ctx context.Context, args map[string]interface{}) extsdk.Result {
	if client == nil {
		return extsdk.Result{Error: "composio not configured"}
	}
	connectionID, _ := args["connection_id"].(string)
	if connectionID == "" {
		return extsdk.Result{Error: "connection_id is required"}
	}
	_, err := client.do(ctx, "POST", "/api/composio/oauth/revoke", map[string]string{
		"connection_id": connectionID,
	})
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("revoke token: %v", err)}
	}
	return extsdk.Result{Success: true, Output: `{"revoked": true}`}
}
