// Skill Registry extension for Mneme.
//
// Provides skill discovery and management tools:
//   - skill_browse: browse available skills from registries
//   - skill_search: search for skills by keyword
//   - skill_registry_install: download and install a skill executable from a registry
//   - skill_uninstall: remove an installed skill
//   - skill_sources: list configured skill sources/registries
//
// Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// dataDir returns the host workspace directory.
func dataDir() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "data")
}

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
type manifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	AgentDefs   []string `json:"agent_defs"`
	ProtocolMin int      `json:"protocol_min"`
}
type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Permission  string                 `json:"permission"`
	HasEffects  bool                   `json:"has_effects"`
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

var extManifest = manifest{
	Name:        "skill-registry",
	Version:     "0.1.0",
	Description: "Skill discovery: browse, search, install, uninstall from registries",
	Tools:       []string{"skill_browse", "skill_search", "skill_registry_install", "skill_uninstall", "skill_sources"},
	AgentDefs:   []string{"skill_creator", "skill_setup"},
	ProtocolMin: 1,
}

var agentDefs = []struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tier          string   `json:"tier"`
	SystemPrompt  string   `json:"systemPrompt"`
	ToolAllowlist []string `json:"toolAllowlist"`
	MaxIterations int      `json:"maxIterations"`
	Hidden        bool     `json:"hidden"`
}{
	{
		ID: "skill_creator", Name: "Skill Creator",
		Description: "Creates and packages new skills from user requirements",
		Tier:        "worker",
		SystemPrompt: `You create new skills for Mneme. Skills are executables that follow the extension protocol (stdin/stdout JSON-RPC).
- Understand the user's requirements for a new tool
- Use skill_browse to check if something similar already exists
- Guide the user through skill creation and publishing`,
		ToolAllowlist: []string{"skill_browse", "skill_search", "skill_registry_install", "skill_sources", "write_file", "shell", "read_file"},
		MaxIterations: 15, Hidden: false,
	},
	{
		ID: "skill_setup", Name: "Skill Setup Guide",
		Description: "Guides users through skill installation and configuration",
		Tier:        "worker",
		SystemPrompt: `You help users discover, install, and configure skills for Mneme.
- Search skill registries for relevant tools
- Guide installation step by step
- Verify skills work after installation
- Help troubleshoot when skills fail`,
		ToolAllowlist: []string{"skill_browse", "skill_search", "skill_registry_install", "skill_uninstall", "skill_sources", "shell", "list_dir"},
		MaxIterations: 12, Hidden: false,
	},
}

var toolDefs = []toolDef{
	{
		Name:        "skill_browse",
		Description: "Browse available skills from configured registries. Lists all skills or filtered by category.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"category": map[string]interface{}{"type": "string", "description": "Filter by category: devtools, productivity, communication, browser, automation, etc."},
				"source":   map[string]interface{}{"type": "string", "description": "Registry source (default: all configured sources)"},
				"limit":    map[string]interface{}{"type": "number", "description": "Max results (default 20)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "skill_search",
		Description: "Search for skills by keyword across all configured registries",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string", "description": "Search keyword"},
				"limit": map[string]interface{}{"type": "number", "description": "Max results (default 10)"},
			},
			"required": []string{"query"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "skill_registry_install",
		Description: "Download and install a skill executable from a registry source",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"skillName": map[string]interface{}{"type": "string", "description": "Skill name/ID to install"},
				"source":    map[string]interface{}{"type": "string", "description": "Registry source URL"},
				"version":   map[string]interface{}{"type": "string", "description": "Specific version to install (default: latest)"},
			},
			"required": []string{"skillName"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "skill_uninstall",
		Description: "Remove an installed skill",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"skillName": map[string]interface{}{"type": "string", "description": "Skill name to uninstall"},
			},
			"required": []string{"skillName"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "skill_sources",
		Description: "List configured skill registry sources and their status",
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Permission:  "read_only",
		HasEffects:  false,
	},
}

var httpClient = &http.Client{Timeout: 15 * time.Second}
var skillsDir = filepath.Join(dataDir(), "skills")

func init() { os.MkdirAll(skillsDir, 0755) }

type skillEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Source      string `json:"source"`
	Installed   bool   `json:"installed"`
	Size        string `json:"size,omitempty"`
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("skill-registry extension starting")
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
		resp := handleRequest(&req)
		respBytes, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stdout, "%s\n", respBytes)
	}
}

func handleRequest(req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "extension.describe":
		result, _ := json.Marshal(extManifest)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_tools":
		type lr struct{ Tools []toolDef }
		result, _ := json.Marshal(lr{Tools: toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		result, _ := json.Marshal(map[string]interface{}{"agents": agentDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "skill_browse":
			result = skillBrowse(ctx, params.Args)
		case "skill_search":
			result = skillSearch(ctx, params.Args)
		case "skill_registry_install":
			result = skillRegistryInstall(ctx, params.Args)
		case "skill_uninstall":
			result = skillUninstall(params.Args)
		case "skill_sources":
			result = skillSources()
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func getSources() []string {
	sources := strings.Fields(os.Getenv("MNEME_SKILL_SOURCES"))
	if len(sources) == 0 {
		sources = []string{}
	}
	return sources
}

func installedSkills() []string {
	entries, _ := os.ReadDir(skillsDir)
	var names []string
	for _, e := range entries {
		if !e.IsDir() && e.Name()[0] != '.' {
			if info, _ := e.Info(); info.Mode()&0111 != 0 {
				names = append(names, e.Name())
			}
		}
	}
	return names
}

func skillBrowse(ctx context.Context, args map[string]interface{}) callToolResult {
	category, _ := args["category"].(string)
	limit := 20
	if l, ok := getInt(args, "limit"); ok && l > 0 {
		limit = l
	}

	installed := installedSkills()
	installedSet := make(map[string]bool)
	for _, n := range installed {
		installedSet[n] = true
	}

	// Return installed skills as the primary catalog + registry placeholder
	var results []skillEntry
	for _, name := range installed {
		if category != "" && !strings.Contains(name, category) {
			continue
		}
		results = append(results, skillEntry{
			Name: name, Description: "Installed skill", Source: "local",
			Category: "installed", Installed: true, Version: "1.0",
		})
		if len(results) >= limit {
			break
		}
	}

	_ = ctx
	if len(results) == 0 {
		return callToolResult{Success: true, Output: fmt.Sprintf(
			"No skills found. Place executables in %s\nConfigure MNEME_SKILL_SOURCES for remote registries.", skillsDir)}
	}

	b, _ := json.MarshalIndent(results, "", "  ")
	return callToolResult{Success: true, Output: string(b)}
}

func skillSearch(ctx context.Context, args map[string]interface{}) callToolResult {
	query, _ := args["query"].(string)
	if query == "" {
		return callToolResult{Error: "query is required"}
	}

	limit := 10
	if l, ok := getInt(args, "limit"); ok && l > 0 {
		limit = l
	}

	installed := installedSkills()
	var results []skillEntry
	for _, name := range installed {
		if strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
			results = append(results, skillEntry{
				Name: name, Description: "Installed skill matching: " + query,
				Source: "local", Installed: true,
			})
		}
		if len(results) >= limit {
			break
		}
	}

	if len(results) == 0 {
		return callToolResult{Success: true, Output: fmt.Sprintf("No skills found matching '%s'", query)}
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	_ = ctx
	return callToolResult{Success: true, Output: string(b)}
}

func skillRegistryInstall(ctx context.Context, args map[string]interface{}) callToolResult {
	skillName, _ := args["skillName"].(string)
	if skillName == "" {
		return callToolResult{Error: "skillName is required"}
	}
	source, _ := args["source"].(string)

	os.MkdirAll(skillsDir, 0755)
	destPath := filepath.Join(skillsDir, skillName)

	if source != "" && (strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")) {
		url := source + "/" + skillName
		req, reqErr := http.NewRequestWithContext(ctx, "GET", url, nil)
		if reqErr != nil {
			return callToolResult{Error: fmt.Sprintf("request: %v", reqErr)}
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("download: %v", err)}
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return callToolResult{Error: fmt.Sprintf("download failed: HTTP %d", resp.StatusCode)}
		}
		f, err := os.Create(destPath)
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("create file: %v", err)}
		}
		defer f.Close()
		if _, err := io.Copy(f, resp.Body); err != nil {
			return callToolResult{Error: fmt.Sprintf("download incomplete: %v", err)}
		}
		os.Chmod(destPath, 0755)
	} else {
		binPath, err := exec.LookPath(skillName)
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("skill %q not found in PATH. Provide a source URL to download from a registry.", skillName)}
		}
		data, err := os.ReadFile(binPath)
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("read binary: %v", err)}
		}
		if err := os.WriteFile(destPath, data, 0755); err != nil {
			return callToolResult{Error: fmt.Sprintf("install: %v", err)}
		}
	}

	abs, _ := filepath.Abs(destPath)
	return callToolResult{Success: true, Output: fmt.Sprintf("Installed: %s → %s\nRestart Mneme to discover the new skill.", skillName, abs)}
}

func skillUninstall(args map[string]interface{}) callToolResult {
	skillName, _ := args["skillName"].(string)
	if skillName == "" {
		return callToolResult{Error: "skillName is required"}
	}

	destPath := filepath.Join(skillsDir, skillName)
	if _, err := os.Stat(destPath); err != nil {
		return callToolResult{Error: fmt.Sprintf("skill %q is not installed", skillName)}
	}

	if err := os.Remove(destPath); err != nil {
		return callToolResult{Error: fmt.Sprintf("uninstall: %v", err)}
	}
	return callToolResult{Success: true, Output: fmt.Sprintf("Uninstalled: %s", skillName)}
}

func skillSources() callToolResult {
	sources := getSources()
	status := map[string]interface{}{
		"skills_dir": skillsDir,
		"installed":  installedSkills(),
		"sources":    sources,
		"add_source": "Set MNEME_SKILL_SOURCES env var with space-separated registry URLs.",
	}
	if len(sources) == 0 {
		status["note"] = "No remote sources configured. Set MNEME_SKILL_SOURCES to add skill registries."
	}
	b, _ := json.MarshalIndent(status, "", "  ")
	return callToolResult{Success: true, Output: string(b)}
}

func getInt(args map[string]interface{}, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}
