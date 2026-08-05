// Package registry provides MCP server discovery from upstream registries
// (Smithery.ai and MCP Official). Results are cached in SQLite with a
// 10-minute TTL, and a static catalog serves as offline fallback.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/simon/mneme/internal/mcp/store"
)

// ServerSummary is a lightweight registry search result.
type ServerSummary struct {
	QualifiedName string `json:"qualified_name"`
	DisplayName   string `json:"display_name"`
	Description   string `json:"description,omitempty"`
	IconURL       string `json:"icon_url,omitempty"`
	UseCount      int64  `json:"use_count"`
	IsDeployed    bool   `json:"is_deployed"`
	Source        string `json:"source"` // "smithery" or "mcp_official"
}

// ServerDetail is a full registry entry with connection info.
type ServerDetail struct {
	QualifiedName string             `json:"qualified_name"`
	DisplayName   string             `json:"display_name"`
	Description   string             `json:"description,omitempty"`
	IconURL       string             `json:"icon_url,omitempty"`
	Connections   []ServerConnection `json:"connections"`
	Source        string             `json:"source"`
}

// ServerConnection describes one transport option for a server.
type ServerConnection struct {
	Type          string                 `json:"type"` // "stdio" or "http"
	DeploymentURL string                 `json:"deployment_url,omitempty"`
	ConfigSchema  map[string]interface{} `json:"config_schema,omitempty"`
	ExampleConfig map[string]interface{} `json:"example_config,omitempty"`
	Published     bool                   `json:"published"`
}

// Client searches MCP registries for available servers.
type Client struct {
	baseURL    string
	httpClient *http.Client
	cache      *store.Store
	log        *slog.Logger
}

// NewClient creates a registry client backed by the MCP store for caching.
func NewClient(cache *store.Store) *Client {
	return &Client{
		baseURL:    "https://registry.smithery.ai",
		httpClient: &http.Client{Timeout: 15 * time.Second},
		cache:      cache,
		log:        slog.Default().With("component", "mcp-registry"),
	}
}

// Search queries the Smithery registry for MCP server packages matching
// the query. Results are cached for 10 minutes. Falls back to the static
// catalog when the remote API is unreachable.
func (c *Client) Search(ctx context.Context, query string) ([]ServerSummary, error) {
	cacheKey := "search:" + query
	if query == "" {
		cacheKey = "search:*"
	}

	// Check cache first.
	if c.cache != nil {
		if cached, ok := c.cache.GetCached(cacheKey); ok {
			var results []ServerSummary
			if err := json.Unmarshal([]byte(cached), &results); err == nil && len(results) > 0 {
				c.log.Debug("registry search cache hit", "query", query, "results", len(results))
				return results, nil
			}
		}
	}

	results, err := c.searchRemote(ctx, query)
	if err != nil {
		c.log.Warn("registry remote search failed, using static fallback", "query", query, "error", err)
		results = c.searchStatic(query)
	}

	// Persist to cache.
	if c.cache != nil && len(results) > 0 {
		data, _ := json.Marshal(results)
		if err := c.cache.SetCached(cacheKey, string(data)); err != nil {
			c.log.Warn("failed to cache registry results", "query", query, "error", err)
		}
	}

	return results, nil
}

// Get fetches a single server detail from the registry.
func (c *Client) Get(ctx context.Context, qualifiedName string) (*ServerDetail, error) {
	cacheKey := "detail:" + qualifiedName
	if c.cache != nil {
		if cached, ok := c.cache.GetCached(cacheKey); ok {
			var detail ServerDetail
			if err := json.Unmarshal([]byte(cached), &detail); err == nil {
				c.log.Debug("registry detail cache hit", "name", qualifiedName)
				return &detail, nil
			}
		}
	}

	detail, err := c.getRemote(ctx, qualifiedName)
	if err != nil {
		// Try static catalog as fallback.
		for _, s := range staticCatalog() {
			if s.Name == qualifiedName {
				return staticToDetail(&s), nil
			}
		}
		return nil, fmt.Errorf("registry get %q: %w", qualifiedName, err)
	}

	if c.cache != nil {
		data, _ := json.Marshal(detail)
		_ = c.cache.SetCached(cacheKey, string(data))
	}
	return detail, nil
}

// ResolveInstall resolves the command, args, and transport for installing
// a server by name. Searches the registry (with cache/fallback) and picks
// the best available connection.
func (c *Client) ResolveInstall(ctx context.Context, qualifiedName string) (*ResolvedInstall, error) {
	detail, err := c.Get(ctx, qualifiedName)
	if err != nil {
		return nil, err
	}
	conn := pickConnection(detail.Connections)
	if conn == nil {
		return nil, fmt.Errorf("server %q has no dialable connections", qualifiedName)
	}
	return buildInstall(qualifiedName, conn), nil
}

// ResolvedInstall carries the resolved install parameters for an MCP server.
type ResolvedInstall struct {
	Transport     string   `json:"transport"`
	Command       string   `json:"command,omitempty"`
	Args          []string `json:"args,omitempty"`
	DeploymentURL string   `json:"deployment_url,omitempty"`
}

func buildInstall(qualifiedName string, conn *ServerConnection) *ResolvedInstall {
	switch normalizeTransport(conn.Type) {
	case "http_remote":
		return &ResolvedInstall{
			Transport:     "http_remote",
			DeploymentURL: conn.DeploymentURL,
		}
	default:
		cmd, args := resolveCommand(qualifiedName, conn)
		return &ResolvedInstall{
			Transport: "stdio",
			Command:   cmd,
			Args:      args,
		}
	}
}

// ── Remote HTTP ─────────────────────────────────────────────────────────

func (c *Client) searchRemote(ctx context.Context, query string) ([]ServerSummary, error) {
	url := fmt.Sprintf("%s/servers/search?q=%s&pageSize=20", c.baseURL, query)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mneme-go/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry search returned %d", resp.StatusCode)
	}

	var apiResp struct {
		Servers []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
			Description string `json:"description"`
			IconURL     string `json:"iconUrl"`
			UseCount    int64  `json:"useCount"`
			IsDeployed  bool   `json:"isDeployed"`
		} `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("registry search decode: %w", err)
	}

	results := make([]ServerSummary, 0, len(apiResp.Servers))
	for _, s := range apiResp.Servers {
		results = append(results, ServerSummary{
			QualifiedName: s.Name,
			DisplayName:   s.DisplayName,
			Description:   s.Description,
			IconURL:       s.IconURL,
			UseCount:      s.UseCount,
			IsDeployed:    s.IsDeployed,
			Source:        "smithery",
		})
	}
	return results, nil
}

func (c *Client) getRemote(ctx context.Context, qualifiedName string) (*ServerDetail, error) {
	url := fmt.Sprintf("%s/servers/%s", c.baseURL, qualifiedName)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mneme-go/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry get returned %d", resp.StatusCode)
	}

	var apiResp struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
		IconURL     string `json:"iconUrl"`
		Connections []struct {
			Type          string                 `json:"type"`
			DeploymentURL string                 `json:"deploymentUrl"`
			ConfigSchema  map[string]interface{} `json:"configSchema"`
			ExampleConfig map[string]interface{} `json:"exampleConfig"`
			Published     bool                   `json:"published"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("registry get decode: %w", err)
	}

	conns := make([]ServerConnection, 0, len(apiResp.Connections))
	for _, c := range apiResp.Connections {
		conns = append(conns, ServerConnection{
			Type:          c.Type,
			DeploymentURL: c.DeploymentURL,
			ConfigSchema:  c.ConfigSchema,
			ExampleConfig: c.ExampleConfig,
			Published:     c.Published,
		})
	}
	return &ServerDetail{
		QualifiedName: apiResp.Name,
		DisplayName:   apiResp.DisplayName,
		Description:   apiResp.Description,
		IconURL:       apiResp.IconURL,
		Connections:   conns,
		Source:        "smithery",
	}, nil
}

// ── Connection picking ───────────────────────────────────────────────────

// pickConnection chooses the best connection: published stdio > any stdio >
// published http_remote > any http_remote.
func pickConnection(conns []ServerConnection) *ServerConnection {
	// Published stdio
	for i, c := range conns {
		if normalizeTransport(c.Type) == "stdio" && c.Published {
			return &conns[i]
		}
	}
	// Any stdio
	for i, c := range conns {
		if normalizeTransport(c.Type) == "stdio" {
			return &conns[i]
		}
	}
	// Published http_remote
	for i, c := range conns {
		if normalizeTransport(c.Type) == "http_remote" && c.Published {
			return &conns[i]
		}
	}
	// Any http_remote
	for i, c := range conns {
		if normalizeTransport(c.Type) == "http_remote" {
			return &conns[i]
		}
	}
	return nil
}

func normalizeTransport(t string) string {
	switch t {
	case "stdio":
		return "stdio"
	case "http", "http_remote", "sse":
		return "http_remote"
	default:
		return t
	}
}

func resolveCommand(qualifiedName string, conn *ServerConnection) (string, []string) {
	if conn != nil && conn.ExampleConfig != nil {
		if cmd, ok := conn.ExampleConfig["command"].(string); ok && cmd != "" {
			var args []string
			if arr, ok := conn.ExampleConfig["args"].([]interface{}); ok {
				for _, a := range arr {
					if s, ok := a.(string); ok {
						args = append(args, s)
					}
				}
			}
			return cmd, args
		}
	}
	return "npx", []string{"-y", qualifiedName}
}

// ── Static catalog (offline fallback) ────────────────────────────────────

type staticEntry struct {
	Name        string
	DisplayName string
	Description string
	Command     string
	Args        []string
	Transport   string
}

func staticCatalog() []staticEntry {
	return []staticEntry{
		{Name: "@modelcontextprotocol/server-filesystem", DisplayName: "Filesystem", Description: "Secure file system access with configurable paths", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem"}, Transport: "stdio"},
		{Name: "@modelcontextprotocol/server-github", DisplayName: "GitHub", Description: "GitHub API integration for repository management, issues, and PRs", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-github"}, Transport: "stdio"},
		{Name: "@modelcontextprotocol/server-postgres", DisplayName: "PostgreSQL", Description: "PostgreSQL database access with read-only query support", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-postgres"}, Transport: "stdio"},
		{Name: "@modelcontextprotocol/server-brave-search", DisplayName: "Brave Search", Description: "Web and local search via Brave Search API", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-brave-search"}, Transport: "stdio"},
		{Name: "@modelcontextprotocol/server-memory", DisplayName: "Memory", Description: "Knowledge graph-based persistent memory system", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-memory"}, Transport: "stdio"},
		{Name: "@modelcontextprotocol/server-puppeteer", DisplayName: "Puppeteer", Description: "Headless browser automation for web scraping and screenshots", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-puppeteer"}, Transport: "stdio"},
		{Name: "@modelcontextprotocol/server-slack", DisplayName: "Slack", Description: "Slack workspace integration for channels and messaging", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-slack"}, Transport: "stdio"},
		{Name: "@modelcontextprotocol/server-google-maps", DisplayName: "Google Maps", Description: "Google Maps geocoding, directions, and places API", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-google-maps"}, Transport: "stdio"},
		{Name: "@modelcontextprotocol/server-everart", DisplayName: "EverArt", Description: "AI image generation via various models", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-everart"}, Transport: "stdio"},
		{Name: "@modelcontextprotocol/server-sqlite", DisplayName: "SQLite", Description: "SQLite database access with full query support", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-sqlite"}, Transport: "stdio"},
		{Name: "@modelcontextprotocol/server-sequential-thinking", DisplayName: "Sequential Thinking", Description: "Multi-step reasoning and problem-solving", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-sequential-thinking"}, Transport: "stdio"},
		{Name: "@modelcontextprotocol/server-fetch", DisplayName: "Fetch", Description: "HTTP content fetching and conversion to markdown", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-fetch"}, Transport: "stdio"},
		{Name: "@playwright/mcp", DisplayName: "Playwright", Description: "Cross-browser automation with Playwright", Command: "npx", Args: []string{"-y", "@playwright/mcp"}, Transport: "stdio"},
		{Name: "@qdrant/mcp-server-qdrant", DisplayName: "Qdrant", Description: "Vector database for semantic search and RAG", Command: "npx", Args: []string{"-y", "@qdrant/mcp-server-qdrant"}, Transport: "stdio"},
		{Name: "@notionhq/notion-mcp-server", DisplayName: "Notion", Description: "Notion workspace API for pages, databases, and blocks", Command: "npx", Args: []string{"-y", "@notionhq/notion-mcp-server"}, Transport: "stdio"},
		{Name: "@airtable/mcp-server", DisplayName: "Airtable", Description: "Airtable base and record management", Command: "npx", Args: []string{"-y", "@airtable/mcp-server"}, Transport: "stdio"},
		{Name: "@stripe/mcp", DisplayName: "Stripe", Description: "Stripe payment processing and subscription management", Command: "npx", Args: []string{"-y", "@stripe/mcp"}, Transport: "stdio"},
		{Name: "@anthropic/mcp-server-exa", DisplayName: "Exa", Description: "AI-powered web search with semantic understanding", Command: "npx", Args: []string{"-y", "@anthropic/mcp-server-exa"}, Transport: "stdio"},
		{Name: "@tavily/mcp-server", DisplayName: "Tavily", Description: "Web search API optimized for AI agents", Command: "npx", Args: []string{"-y", "@tavily/mcp-server"}, Transport: "stdio"},
		{Name: "@linear/mcp-server", DisplayName: "Linear", Description: "Linear project management and issue tracking", Command: "npx", Args: []string{"-y", "@linear/mcp-server"}, Transport: "stdio"},
	}
}

func (c *Client) searchStatic(query string) []ServerSummary {
	all := staticCatalog()
	q := strings.ToLower(query)
	var results []ServerSummary
	for _, s := range all {
		if q == "" ||
			strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.Description), q) {
			results = append(results, ServerSummary{
				QualifiedName: s.Name,
				DisplayName:   s.DisplayName,
				Description:   s.Description,
				Source:        "smithery",
			})
		}
	}
	return results
}

func staticToDetail(e *staticEntry) *ServerDetail {
	return &ServerDetail{
		QualifiedName: e.Name,
		DisplayName:   e.DisplayName,
		Description:   e.Description,
		Connections: []ServerConnection{
			{Type: e.Transport, Published: true},
		},
		Source: "smithery",
	}
}
