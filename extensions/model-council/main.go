// Model Council extension for Mneme.
//
// Sends the same prompt to multiple LLM backends and returns aggregated results.
// Each council member is configured via environment variables:
//
//	COUNCIL_MODELS: comma-separated model names (e.g. "claude-sonnet-4-6,deepseek-v4-pro")
//	COUNCIL_BASE_URL: API base URL (Anthropic-compatible endpoint)
//	COUNCIL_API_KEY: API key
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
	"strings"

	"github.com/simon/mneme/pkg/extsdk"
)

// ── Main ──────────────────────────────────────────────────────────────────

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "model-council",
		Version:     "0.1.0",
		Description: "Multi-model deliberation: query multiple LLMs and aggregate their responses",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "model_council_deliberate",
		Description: "Send the same prompt to multiple configured models and return all responses for comparison.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "The question or prompt to send to all council members.",
				},
			},
			"required": []string{"prompt"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, deliberate)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "model-council: %v\n", err)
		os.Exit(1)
	}
}

// ── Council logic ──────────────────────────────────────────────────────────

type councilMember struct {
	Model string `json:"model"`
	Reply string `json:"reply"`
	Error string `json:"error,omitempty"`
}

func deliberate(ctx context.Context, args map[string]interface{}) extsdk.Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return extsdk.Result{Error: "prompt is required"}
	}

	baseURL := os.Getenv("COUNCIL_BASE_URL")
	apiKey := os.Getenv("COUNCIL_API_KEY")
	modelsStr := os.Getenv("COUNCIL_MODELS")

	if baseURL == "" || apiKey == "" || modelsStr == "" {
		return extsdk.Result{Error: "not configured. Set COUNCIL_BASE_URL, COUNCIL_API_KEY, and COUNCIL_MODELS env vars."}
	}

	models := strings.Split(modelsStr, ",")
	var members []councilMember

	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		reply, err := queryModel(ctx, baseURL, apiKey, model, prompt)
		m := councilMember{Model: model}
		if err != nil {
			m.Error = err.Error()
		} else {
			m.Reply = reply
		}
		members = append(members, m)
	}

	out, _ := json.MarshalIndent(map[string]interface{}{
		"prompt":      prompt,
		"council":     members,
		"memberCount": len(members),
	}, "", "  ")
	return extsdk.Result{Success: true, Output: string(out)}
}

func queryModel(ctx context.Context, baseURL, apiKey, model, prompt string) (string, error) {
	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 512,
		"messages": []map[string]interface{}{
			{"role": "user", "content": prompt},
		},
	}
	body, _ := json.Marshal(reqBody)

	url := strings.TrimRight(baseURL, "/") + "/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return string(respBody), nil // return raw response if parsing fails
	}
	var texts []string
	for _, c := range result.Content {
		if c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	return strings.Join(texts, "\n"), nil
}
