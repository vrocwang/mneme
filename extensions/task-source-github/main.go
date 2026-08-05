// Task Source: GitHub extension for Mneme.
//
// Provides tools for syncing tasks from GitHub Issues and Pull Requests.
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
	"strings"
	"time"
)

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

type callToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

type toolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Permission  string          `json:"permission"`
	HasEffects  bool            `json:"has_effects"`
}

var toolDefs = []toolDef{
	toolDef{Name: "github_sync_issues", Description: "Sync tasks from GitHub Issues and Pull Requests.", Parameters: json.RawMessage("{\"type\":\"object\",\"properties\":{\"repo\":{\"type\":\"string\",\"description\":\"Repository name (owner/name)\"},\"token\":{\"type\":\"string\",\"description\":\"GitHub personal access token (or set GITHUB_TOKEN env var)\"},\"state\":{\"type\":\"string\",\"description\":\"Issue state: open, closed, or all. Default: open\"}},\"required\":[\"repo\"]}")},
	toolDef{Name: "github_list_issues", Description: "List GitHub Issues for a repository (alias for sync_issues).", Parameters: json.RawMessage("{\"type\":\"object\",\"properties\":{\"repo\":{\"type\":\"string\",\"description\":\"Repository name (owner/name)\"},\"token\":{\"type\":\"string\",\"description\":\"GitHub personal access token (or set GITHUB_TOKEN env var)\"}},\"required\":[\"repo\"]}")},
}

var extManifest = manifest{
	Name:        "task-source-github",
	Version:     "0.1.0",
	Description: "Sync tasks from GitHub Issues and Pull Requests",
	Tools:       []string{"github_sync_issues", "github_list_issues"},
	ProtocolMin: 1,
}

type githubLabel struct {
	Name string `json:"name"`
}

type githubIssue struct {
	Number    int           `json:"number"`
	Title     string        `json:"title"`
	State     string        `json:"state"`
	URL       string        `json:"html_url"`
	Labels    []githubLabel `json:"labels"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	scanner := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			log.Warn("bad request", "error", err)
			continue
		}
		resp := dispatch(req)
		data, _ := json.Marshal(resp)
		writer.WriteString(string(data) + "\n")
		writer.Flush()
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		log.Error("stdin error", "error", err)
	}
}

func dispatch(req rpcRequest) rpcResponse {
	switch req.Method {
	case "manifest", "extension.describe":
		return okRPC(req.ID, extManifest)
	case "extension.list_agents":
		result, _ := json.Marshal(map[string]interface{}{"agents": []interface{}{}})
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_tools":
		result, _ := json.Marshal(map[string]interface{}{"tools": toolDefs})
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var p struct {
			Name string                 `json:"name"`
			Args map[string]interface{} `json:"args"`
		}
		json.Unmarshal(req.Params, &p)
		result := callTool(p.Name, p.Args)
		return okRPC(req.ID, result)
	default:
		return errRPC(req.ID, -32601, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func callTool(name string, args map[string]interface{}) callToolResult {
	switch name {
	case "github_sync_issues":
		return syncIssues(args)
	case "github_list_issues":
		return listIssues(args)
	default:
		return callToolResult{Success: false, Error: fmt.Sprintf("unknown tool: %s", name)}
	}
}

func syncIssues(args map[string]interface{}) callToolResult {
	repo, _ := args["repo"].(string)
	token, _ := args["token"].(string)
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if repo == "" {
		return callToolResult{Success: false, Error: "repo is required (owner/name)"}
	}
	if token == "" {
		return callToolResult{Success: false, Error: "GitHub token is required"}
	}

	state, _ := args["state"].(string)
	if state == "" {
		state = "open"
	}

	issues, err := fetchIssues(repo, token, state)
	if err != nil {
		return callToolResult{Success: false, Error: err.Error()}
	}

	tasks := make([]map[string]interface{}, 0, len(issues))
	for _, iss := range issues {
		labels := make([]string, 0, len(iss.Labels))
		for _, l := range iss.Labels {
			labels = append(labels, l.Name)
		}
		tasks = append(tasks, map[string]interface{}{
			"source":     "github",
			"source_id":  fmt.Sprintf("%d", iss.Number),
			"title":      iss.Title,
			"status":     iss.State,
			"url":        iss.URL,
			"labels":     labels,
			"created_at": iss.CreatedAt,
			"updated_at": iss.UpdatedAt,
		})
	}

	outputData, _ := json.Marshal(map[string]interface{}{
		"count": len(tasks),
		"tasks": tasks,
	})
	return callToolResult{Success: true, Output: string(outputData)}
}

func listIssues(args map[string]interface{}) callToolResult {
	return syncIssues(args) // same behavior for now
}

func fetchIssues(repo, token, state string) ([]githubIssue, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues?state=%s&per_page=50", repo, state)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "mneme-task-source-github")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var issues []githubIssue
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return issues, nil
}

func okRPC(id int64, v interface{}) rpcResponse {
	data, _ := json.Marshal(v)
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: data}
}
func errRPC(id int64, code int, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}
