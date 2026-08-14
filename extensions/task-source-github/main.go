// Task Source: GitHub extension for Mneme.
//
// Provides tools for syncing tasks from GitHub Issues and Pull Requests.
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

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
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "task-source-github",
		Version:     "0.1.0",
		Description: "Sync tasks from GitHub Issues and Pull Requests",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "github_sync_issues",
		Description: "Sync tasks from GitHub Issues and Pull Requests.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"repo":  map[string]interface{}{"type": "string", "description": "Repository name (owner/name)"},
				"token": map[string]interface{}{"type": "string", "description": "GitHub personal access token (or set GITHUB_TOKEN env var)"},
				"state": map[string]interface{}{"type": "string", "description": "Issue state: open, closed, or all. Default: open"},
			},
			"required": []string{"repo"},
		},
	}, syncIssues)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "github_list_issues",
		Description: "List GitHub Issues for a repository (alias for sync_issues).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"repo":  map[string]interface{}{"type": "string", "description": "Repository name (owner/name)"},
				"token": map[string]interface{}{"type": "string", "description": "GitHub personal access token (or set GITHUB_TOKEN env var)"},
			},
			"required": []string{"repo"},
		},
	}, listIssues)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "task-source-github: %v\n", err)
		os.Exit(1)
	}
}

func syncIssues(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = ctx
	repo, _ := args["repo"].(string)
	token, _ := args["token"].(string)
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if repo == "" {
		return extsdk.Result{Success: false, Error: "repo is required (owner/name)"}
	}
	if token == "" {
		return extsdk.Result{Success: false, Error: "GitHub token is required"}
	}

	state, _ := args["state"].(string)
	if state == "" {
		state = "open"
	}

	issues, err := fetchIssues(repo, token, state)
	if err != nil {
		return extsdk.Result{Success: false, Error: err.Error()}
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
	return extsdk.Result{Success: true, Output: string(outputData)}
}

func listIssues(ctx context.Context, args map[string]interface{}) extsdk.Result {
	return syncIssues(ctx, args) // same behavior for now
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
