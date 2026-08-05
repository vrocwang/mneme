// Composio extension for Mneme.
//
// Provides OAuth SaaS integration tools via the Composio backend API.
// Communicates via the Mneme extension protocol (stdin/stdout JSON-RPC 2.0).
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
	"bufio"
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
)

// ── Extension protocol types ────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ── Manifest and tool definitions ─────────────────────────────────────────

type manifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	AgentDefs   []string `json:"agent_defs"`
	ProtocolMin int      `json:"protocol_min"`
}

var extManifest = manifest{
	Name:        "composio",
	Version:     "0.1.0",
	Description: "Composio OAuth SaaS integrations",
	Tools:       []string{"composio_list_toolkits", "composio_execute_action", "composio_list_connections", "composio_start_oauth", "composio_complete_oauth", "composio_refresh_token", "composio_revoke_token"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Permission  string                 `json:"permission"`
	HasEffects  bool                   `json:"has_effects"`
}

var toolDefs = []toolDef{
	{
		Name:        "composio_list_toolkits",
		Description: "List available Composio integration toolkits (Gmail, Slack, GitHub, Notion, etc.)",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
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
	},
	{
		Name:        "composio_list_connections",
		Description: "List active OAuth connections for the current user",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
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
	},
	{
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
	},
	{
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
	},
	{
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
	},
}

type callToolParams struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}
type callToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

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

// ── Main loop ─────────────────────────────────────────────────────────────

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

	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
		var req rpcRequest
		json.Unmarshal(line, &req)
		resp := handleRequest(&req, log)
		respBytes, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stdout, "%s\n", respBytes)
	}
}

func handleRequest(req *rpcRequest, log *slog.Logger) *rpcResponse {
	switch req.Method {
	case "extension.describe":
		result, _ := json.Marshal(extManifest)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_tools":
		result, _ := json.Marshal(map[string]interface{}{"tools": toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		result, _ := json.Marshal(map[string]interface{}{"agents": []interface{}{}})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.configure":
		var cfg map[string]interface{}
		json.Unmarshal(req.Params, &cfg)
		if url, ok := cfg["backend_url"].(string); ok {
			if key, ok := cfg["api_key"].(string); ok {
				client = newClient(url, key)
				log.Info("composio configured via extension.configure")
				result, _ := json.Marshal(map[string]bool{"accepted": true})
				return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
			}
		}
		result, _ := json.Marshal(map[string]interface{}{"accepted": false, "error": "backend_url and api_key required"})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "composio_list_toolkits":
			result = listToolkits(ctx)
		case "composio_execute_action":
			result = executeAction(ctx, params.Args)
		case "composio_list_connections":
			result = listConnections(ctx)
		case "composio_start_oauth":
			result = startOAuth(params.Args)
		case "composio_complete_oauth":
			result = completeOAuth(ctx, params.Args)
		case "composio_refresh_token":
			result = refreshToken(ctx, params.Args)
		case "composio_revoke_token":
			result = revokeToken(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown tool: %s", params.Name)}
		}
		res, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown method: %s", req.Method)}}
	}
}

// ── Tool implementations ──────────────────────────────────────────────────

func listToolkits(ctx context.Context) callToolResult {
	if client == nil {
		return callToolResult{Error: "composio not configured. Set COMPOSIO_BACKEND_URL and COMPOSIO_API_KEY."}
	}
	body, err := client.do(ctx, "GET", "/api/composio/toolkits", nil)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("list toolkits: %v", err)}
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
		return callToolResult{Error: fmt.Sprintf("parse: %v", err)}
	}
	b, _ := json.MarshalIndent(resp.Toolkits, "", "  ")
	return callToolResult{Success: true, Output: string(b)}
}

func executeAction(ctx context.Context, args map[string]interface{}) callToolResult {
	if client == nil {
		return callToolResult{Error: "composio not configured"}
	}
	toolkit, _ := args["toolkit"].(string)
	action, _ := args["action"].(string)
	params, _ := args["params"].(map[string]interface{})
	if toolkit == "" || action == "" {
		return callToolResult{Error: "toolkit and action required"}
	}
	if params == nil {
		params = map[string]interface{}{}
	}
	req := map[string]interface{}{"toolkit": toolkit, "action": action, "params": params}
	body, err := client.do(ctx, "POST", "/api/composio/actions/execute", req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("execute: %v", err)}
	}
	var resp struct {
		Success bool   `json:"success"`
		Output  string `json:"output"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return callToolResult{Error: fmt.Sprintf("parse response: %v", err)}
	}
	return callToolResult{Success: resp.Success, Output: resp.Output, Error: resp.Error}
}

func listConnections(ctx context.Context) callToolResult {
	if client == nil {
		return callToolResult{Error: "composio not configured"}
	}
	body, err := client.do(ctx, "GET", "/api/composio/connections", nil)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("list connections: %v", err)}
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
		return callToolResult{Error: fmt.Sprintf("parse response: %v", err)}
	}
	b, _ := json.MarshalIndent(resp.Connections, "", "  ")
	return callToolResult{Success: true, Output: string(b)}
}

func startOAuth(args map[string]interface{}) callToolResult {
	if client == nil {
		return callToolResult{Error: "composio not configured"}
	}
	app, _ := args["app"].(string)
	redirectURI, _ := args["redirect_uri"].(string)
	if app == "" {
		return callToolResult{Error: "app is required"}
	}
	state, err := generateState()
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("generate state: %v", err)}
	}
	oauthStates[state] = app
	_ = saveOAuthStates(oauthStates) // best-effort persist for crash recovery

	body, err := client.do(context.Background(), "POST", "/api/composio/oauth/start", map[string]string{
		"app": app, "redirect_uri": redirectURI, "state": state,
	})
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("start oauth: %v", err)}
	}
	var resp struct {
		AuthURL string `json:"auth_url"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return callToolResult{Error: fmt.Sprintf("parse response: %v", err)}
	}
	b, _ := json.Marshal(map[string]string{"auth_url": resp.AuthURL, "state": state})
	return callToolResult{Success: true, Output: string(b)}
}

func completeOAuth(ctx context.Context, args map[string]interface{}) callToolResult {
	if client == nil {
		return callToolResult{Error: "composio not configured"}
	}
	state, _ := args["state"].(string)
	code, _ := args["code"].(string)
	if state == "" || code == "" {
		return callToolResult{Error: "state and code required"}
	}
	body, err := client.do(ctx, "POST", "/api/composio/oauth/complete", map[string]string{
		"state": state, "code": code,
	})
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("complete oauth: %v", err)}
	}

	// Clean up the used state from memory and disk.
	delete(oauthStates, state)
	_ = saveOAuthStates(oauthStates)

	var resp struct {
		ConnectionID string `json:"connection_id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return callToolResult{Error: fmt.Sprintf("parse response: %v", err)}
	}
	b, _ := json.Marshal(map[string]string{"connection_id": resp.ConnectionID})
	return callToolResult{Success: true, Output: string(b)}
}

func refreshToken(ctx context.Context, args map[string]interface{}) callToolResult {
	if client == nil {
		return callToolResult{Error: "composio not configured"}
	}
	connectionID, _ := args["connection_id"].(string)
	if connectionID == "" {
		return callToolResult{Error: "connection_id is required"}
	}
	body, err := client.do(ctx, "POST", "/api/composio/oauth/refresh", map[string]string{
		"connection_id": connectionID,
	})
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("refresh token: %v", err)}
	}
	var resp struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   string `json:"expires_at,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return callToolResult{Error: fmt.Sprintf("parse response: %v", err)}
	}
	b, _ := json.Marshal(map[string]string{
		"access_token": resp.AccessToken,
		"expires_at":   resp.ExpiresAt,
	})
	return callToolResult{Success: true, Output: string(b)}
}

func revokeToken(ctx context.Context, args map[string]interface{}) callToolResult {
	if client == nil {
		return callToolResult{Error: "composio not configured"}
	}
	connectionID, _ := args["connection_id"].(string)
	if connectionID == "" {
		return callToolResult{Error: "connection_id is required"}
	}
	_, err := client.do(ctx, "POST", "/api/composio/oauth/revoke", map[string]string{
		"connection_id": connectionID,
	})
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("revoke token: %v", err)}
	}
	return callToolResult{Success: true, Output: `{"revoked": true}`}
}
