// Task Source: Linear extension for Mneme.
//
// Provides tools for syncing tasks from Linear issues.
// Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
package main

import (
	"bufio"
	"bytes"
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
	toolDef{Name: "linear_sync_issues", Description: "Sync tasks from Linear via the GraphQL API.", Parameters: json.RawMessage("{\"type\":\"object\",\"properties\":{\"api_key\":{\"type\":\"string\",\"description\":\"Linear API key (or set LINEAR_API_KEY env var)\"},\"team_id\":{\"type\":\"string\",\"description\":\"Linear team ID to fetch issues from\"}},\"required\":[\"api_key\"]}")},
}

var extManifest = manifest{
	Name:        "task-source-linear",
	Version:     "0.1.0",
	Description: "Sync tasks from Linear issues",
	Tools:       []string{"linear_sync_issues"},
	ProtocolMin: 1,
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
	case "linear_sync_issues":
		return syncIssues(args)
	default:
		return callToolResult{Success: false, Error: fmt.Sprintf("unknown tool: %s", name)}
	}
}

func syncIssues(args map[string]interface{}) callToolResult {
	apiKey, _ := args["api_key"].(string)
	if apiKey == "" {
		apiKey = os.Getenv("LINEAR_API_KEY")
	}
	teamID, _ := args["team_id"].(string)

	if apiKey == "" {
		return callToolResult{Success: false, Error: "Linear API key is required"}
	}

	query := `query Issues($teamId: String) {
		issues(first: 50, filter: { state: { name: { in: ["Todo", "In Progress"] } }, team: { id: { eq: $teamId } } }) {
			nodes { id title state { name } url labels { nodes { name } } createdAt updatedAt }
		}
	}`

	variables := map[string]interface{}{}
	if teamID != "" {
		variables["teamId"] = teamID
	}

	body := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return callToolResult{Success: false, Error: fmt.Sprintf("marshal request body: %v", err)}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.linear.app/graphql", bytes.NewReader(bodyJSON))
	if err != nil {
		return callToolResult{Success: false, Error: err.Error()}
	}
	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return callToolResult{Success: false, Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return callToolResult{Success: false, Error: fmt.Sprintf("Linear API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))}
	}

	var result struct {
		Data struct {
			Issues struct {
				Nodes []struct {
					ID        string                                  `json:"id"`
					Title     string                                  `json:"title"`
					State     struct{ Name string }                   `json:"state"`
					URL       string                                  `json:"url"`
					Labels    struct{ Nodes []struct{ Name string } } `json:"labels"`
					CreatedAt string                                  `json:"createdAt"`
					UpdatedAt string                                  `json:"updatedAt"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return callToolResult{Success: false, Error: fmt.Sprintf("parse response: %v", err)}
	}

	if len(result.Errors) > 0 {
		return callToolResult{Success: false, Error: result.Errors[0].Message}
	}

	tasks := make([]map[string]interface{}, 0, len(result.Data.Issues.Nodes))
	for _, iss := range result.Data.Issues.Nodes {
		labels := make([]string, 0)
		for _, l := range iss.Labels.Nodes {
			labels = append(labels, l.Name)
		}
		tasks = append(tasks, map[string]interface{}{
			"source":     "linear",
			"source_id":  iss.ID,
			"title":      iss.Title,
			"status":     iss.State.Name,
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

func okRPC(id int64, v interface{}) rpcResponse {
	data, _ := json.Marshal(v)
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: data}
}
func errRPC(id int64, code int, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}
