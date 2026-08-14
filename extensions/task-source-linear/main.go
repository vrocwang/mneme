// Task Source: Linear extension for Mneme.
//
// Provides tools for syncing tasks from Linear issues.
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
	"strings"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "task-source-linear",
		Version:     "0.1.0",
		Description: "Sync tasks from Linear issues",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "linear_sync_issues",
		Description: "Sync tasks from Linear via the GraphQL API.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"api_key": map[string]interface{}{"type": "string", "description": "Linear API key (or set LINEAR_API_KEY env var)"},
				"team_id": map[string]interface{}{"type": "string", "description": "Linear team ID to fetch issues from"},
			},
			"required": []string{"api_key"},
		},
	}, syncIssues)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "task-source-linear: %v\n", err)
		os.Exit(1)
	}
}

func syncIssues(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = ctx
	apiKey, _ := args["api_key"].(string)
	if apiKey == "" {
		apiKey = os.Getenv("LINEAR_API_KEY")
	}
	teamID, _ := args["team_id"].(string)

	if apiKey == "" {
		return extsdk.Result{Success: false, Error: "Linear API key is required"}
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
		return extsdk.Result{Success: false, Error: fmt.Sprintf("marshal request body: %v", err)}
	}

	reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", "https://api.linear.app/graphql", bytes.NewReader(bodyJSON))
	if err != nil {
		return extsdk.Result{Success: false, Error: err.Error()}
	}
	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return extsdk.Result{Success: false, Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return extsdk.Result{Success: false, Error: fmt.Sprintf("Linear API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))}
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
		return extsdk.Result{Success: false, Error: fmt.Sprintf("parse response: %v", err)}
	}

	if len(result.Errors) > 0 {
		return extsdk.Result{Success: false, Error: result.Errors[0].Message}
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
	return extsdk.Result{Success: true, Output: string(outputData)}
}
