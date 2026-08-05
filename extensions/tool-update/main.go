// Tool Update extension for Mneme.
//
// Provides:
//   - update_check: check current version against latest release
//   - update_apply: download and apply the latest update
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
	"path/filepath"
	"runtime"
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
	Name:        "tool-update",
	Version:     "0.1.0",
	Description: "Check for and apply Mneme updates from GitHub releases",
	Tools:       []string{"update_check", "update_apply"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "update_check",
		Description: "Check current Mneme version against the latest GitHub release",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"repo": map[string]interface{}{"type": "string", "description": "GitHub repo in owner/name format (default: simon/mneme-go)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "update_apply",
		Description: "Download and apply the latest Mneme update from GitHub releases",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"repo":    map[string]interface{}{"type": "string", "description": "GitHub repo in owner/name format (default: simon/mneme-go)"},
				"version": map[string]interface{}{"type": "string", "description": "Specific version to install (default: latest)"},
			},
			"required": []string{},
		},
		Permission: "execute",
		HasEffects: true,
	},
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Current version — can be overridden at build time with ldflags
var currentVersion = "0.1.0"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("tool-update extension starting")
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
		result, _ := json.Marshal(map[string]interface{}{"agents": []interface{}{}})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "update_check":
			result = updateCheck(ctx, params.Args)
		case "update_apply":
			result = updateApply(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func getRepo(args map[string]interface{}) string {
	repo, _ := args["repo"].(string)
	if repo == "" {
		repo = "simon/mneme-go"
	}
	return repo
}

func updateCheck(ctx context.Context, args map[string]interface{}) callToolResult {
	repo := getRepo(args)
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("request: %v", err)}
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("GitHub API: %v", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return callToolResult{Error: fmt.Sprintf("GitHub API error %d: %s", resp.StatusCode, truncate(string(body), 300))}
	}

	var release struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		PublishedAt string `json:"published_at"`
		HTMLURL     string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return callToolResult{Error: fmt.Sprintf("parse: %v", err)}
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	updateAvailable := latestVersion != currentVersion && latestVersion != ""

	result := map[string]interface{}{
		"current":          currentVersion,
		"latest":           latestVersion,
		"release_name":     release.Name,
		"published":        release.PublishedAt,
		"release_url":      release.HTMLURL,
		"update_available": updateAvailable,
		"os":               runtime.GOOS,
		"arch":             runtime.GOARCH,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return callToolResult{Success: true, Output: string(b)}
}

func updateApply(ctx context.Context, args map[string]interface{}) callToolResult {
	repo := getRepo(args)
	targetVersion, _ := args["version"].(string)

	// Determine the asset name based on OS/arch
	assetSuffix := ""
	switch runtime.GOOS {
	case "linux":
		assetSuffix = "linux_amd64"
	case "darwin":
		assetSuffix = "darwin_amd64"
	default:
		return callToolResult{Error: fmt.Sprintf("unsupported OS: %s", runtime.GOOS)}
	}

	if runtime.GOARCH == "arm64" {
		assetSuffix = strings.Replace(assetSuffix, "amd64", "arm64", 1)
	}

	// Get latest release info
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	if targetVersion != "" {
		ver := strings.TrimPrefix(targetVersion, "v")
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/v%s", repo, ver)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("GitHub API: %v", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return callToolResult{Error: fmt.Sprintf("GitHub API error %d", resp.StatusCode)}
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return callToolResult{Error: fmt.Sprintf("parse: %v", err)}
	}

	// Find matching asset
	var downloadURL string
	for _, a := range release.Assets {
		if strings.Contains(a.Name, assetSuffix) {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return callToolResult{Error: fmt.Sprintf("no asset found for %s (available: %d assets)", assetSuffix, len(release.Assets))}
	}

	// Download the asset
	downloadDir := filepath.Join(dataDir(), "updates")
	os.MkdirAll(downloadDir, 0755)
	destPath := filepath.Join(downloadDir, "mneme-"+release.TagName)

	dlReq, reqErr := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if reqErr != nil {
		return callToolResult{Error: fmt.Sprintf("create download request: %v", reqErr)}
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		dlReq.Header.Set("Authorization", "Bearer "+token)
	}
	dlResp, err := httpClient.Do(dlReq)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("download: %v", err)}
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode >= 400 {
		return callToolResult{Error: fmt.Sprintf("download failed: HTTP %d", dlResp.StatusCode)}
	}

	f, err := os.Create(destPath)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("create file: %v", err)}
	}
	defer f.Close()

	written, copyErr := io.Copy(f, dlResp.Body)
	if copyErr != nil {
		return callToolResult{Error: fmt.Sprintf("download incomplete: %v", copyErr)}
	}
	os.Chmod(destPath, 0755)

	// Verify downloaded binary is non-empty.
	data, readErr := os.ReadFile(destPath)
	if readErr != nil {
		return callToolResult{Error: fmt.Sprintf("read downloaded binary: %v", readErr)}
	}
	if len(data) == 0 {
		return callToolResult{Error: "downloaded binary is empty — update aborted"}
	}

	// Try to install: use atomic rename pattern (write to temp, then rename).
	execPath, _ := os.Executable()
	execDir := filepath.Dir(execPath)
	targetPath := filepath.Join(execDir, "mneme-go")

	var installMsg string
	if execPath != "" {
		// Write to temp file first, then rename atomically.
		tmpPath := targetPath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0755); err != nil {
			installMsg = fmt.Sprintf("Could not write %s: %v. New binary at %s.", tmpPath, err, destPath)
		} else if err := os.Rename(tmpPath, targetPath); err != nil {
			installMsg = fmt.Sprintf("Could not replace %s: %v. New binary at %s.", targetPath, err, destPath)
		} else {
			installMsg = fmt.Sprintf("Updated: %s -> %s (%d bytes)", execPath, targetPath, written)
		}
	} else {
		installMsg = fmt.Sprintf("Downloaded to %s (%d bytes). Replace your current binary manually.", destPath, written)
	}

	result := map[string]interface{}{
		"version":  release.TagName,
		"asset":    assetSuffix,
		"download": downloadURL,
		"status":   installMsg,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return callToolResult{Success: true, Output: string(b)}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n...[truncated]"
	}
	return s
}
