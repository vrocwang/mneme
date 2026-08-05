// Task Source: Notion extension for Mneme.
//
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
	ProtocolMin int      `json:"protocol_min"`
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

type toolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Permission  string          `json:"permission"`
	HasEffects  bool            `json:"has_effects"`
}

var toolDefs = []toolDef{
	toolDef{Name: "notion_sync_tasks", Description: "Sync tasks from a Notion database via the Notion API.", Parameters: json.RawMessage("{\"type\":\"object\",\"properties\":{\"api_key\":{\"type\":\"string\",\"description\":\"Notion integration token (or set NOTION_API_KEY env var)\"},\"database_id\":{\"type\":\"string\",\"description\":\"Notion database ID to sync tasks from\"}},\"required\":[\"api_key\",\"database_id\"]}")},
}

var extManifest = manifest{
	Name:        "task-source-notion",
	Version:     "0.1.0",
	Description: "Sync tasks from Notion databases",
	Tools:       []string{"notion_sync_tasks"},
	ProtocolMin: 1,
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

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
		var p callToolParams
		json.Unmarshal(req.Params, &p)
		result := callTool(p.Name, p.Args)
		return okRPC(req.ID, result)
	default:
		return errRPC(req.ID, -32601, fmt.Sprintf("unknown: %s", req.Method))
	}
}

func callTool(name string, args map[string]interface{}) callToolResult {
	switch name {
	case "notion_sync_tasks":
		return syncTasks(args)
	default:
		return callToolResult{Error: fmt.Sprintf("unknown tool: %s", name)}
	}
}

type notionProperty struct {
	Type   string                                    `json:"type"`
	Title  []struct{ Text struct{ Content string } } `json:"title,omitempty"`
	Select *struct{ Name string }                    `json:"select,omitempty"`
	Status *struct{ Name string }                    `json:"status,omitempty"`
}

type notionPage struct {
	ID          string                    `json:"id"`
	URL         string                    `json:"url"`
	CreatedTime string                    `json:"created_time"`
	Properties  map[string]notionProperty `json:"properties"`
}

type notionQueryResponse struct {
	Results []notionPage `json:"results"`
}

func syncTasks(args map[string]interface{}) callToolResult {
	apiKey, _ := args["api_key"].(string)
	if apiKey == "" {
		apiKey = os.Getenv("NOTION_API_KEY")
	}
	databaseID, _ := args["database_id"].(string)

	if databaseID == "" {
		return callToolResult{Error: "database_id is required"}
	}
	if apiKey == "" {
		return callToolResult{Error: "Notion API key is required (api_key or NOTION_API_KEY env)"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://api.notion.com/v1/databases/"+databaseID+"/query",
		bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("create request: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("Notion API: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return callToolResult{Error: fmt.Sprintf("Notion API %d: %s", resp.StatusCode, string(body))}
	}

	var result notionQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return callToolResult{Error: fmt.Sprintf("parse: %v", err)}
	}

	tasks := make([]map[string]interface{}, 0, len(result.Results))
	for _, page := range result.Results {
		title := "Untitled"
		status := "unknown"
		for _, prop := range page.Properties {
			if len(prop.Title) > 0 {
				title = prop.Title[0].Text.Content
			}
			if prop.Select != nil {
				status = prop.Select.Name
			}
			if prop.Status != nil {
				status = prop.Status.Name
			}
		}
		tasks = append(tasks, map[string]interface{}{
			"source":     "notion",
			"source_id":  page.ID,
			"title":      title,
			"status":     status,
			"url":        page.URL,
			"created_at": page.CreatedTime,
		})
	}

	b, _ := json.MarshalIndent(map[string]interface{}{
		"success": true, "count": len(tasks), "tasks": tasks,
	}, "", "  ")
	return callToolResult{Success: true, Output: string(b)}
}

func okRPC(id int64, v interface{}) rpcResponse {
	data, _ := json.Marshal(v)
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: data}
}
func errRPC(id int64, code int, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}
