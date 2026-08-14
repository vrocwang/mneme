// Task Source: Notion extension for Mneme.
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

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

var httpClient = &http.Client{Timeout: 30 * time.Second}

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "task-source-notion",
		Version:     "0.1.0",
		Description: "Sync tasks from Notion databases",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "notion_sync_tasks",
		Description: "Sync tasks from a Notion database via the Notion API.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"api_key":     map[string]interface{}{"type": "string", "description": "Notion integration token (or set NOTION_API_KEY env var)"},
				"database_id": map[string]interface{}{"type": "string", "description": "Notion database ID to sync tasks from"},
			},
			"required": []string{"api_key", "database_id"},
		},
	}, syncTasks)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "task-source-notion: %v\n", err)
		os.Exit(1)
	}
}

func syncTasks(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = ctx
	apiKey, _ := args["api_key"].(string)
	if apiKey == "" {
		apiKey = os.Getenv("NOTION_API_KEY")
	}
	databaseID, _ := args["database_id"].(string)

	if databaseID == "" {
		return extsdk.Result{Error: "database_id is required"}
	}
	if apiKey == "" {
		return extsdk.Result{Error: "Notion API key is required (api_key or NOTION_API_KEY env)"}
	}

	reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST",
		"https://api.notion.com/v1/databases/"+databaseID+"/query",
		bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("create request: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("Notion API: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return extsdk.Result{Error: fmt.Sprintf("Notion API %d: %s", resp.StatusCode, string(body))}
	}

	var result notionQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return extsdk.Result{Error: fmt.Sprintf("parse: %v", err)}
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
	return extsdk.Result{Success: true, Output: string(b)}
}
