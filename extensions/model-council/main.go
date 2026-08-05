// Model Council extension for Mneme.
//
// Sends the same prompt to multiple LLM backends and returns aggregated results.
// Each council member is configured via environment variables:
//
//	COUNCIL_MODELS: comma-separated model names (e.g. "claude-sonnet-4-6,deepseek-v4-pro")
//	COUNCIL_BASE_URL: API base URL (Anthropic-compatible endpoint)
//	COUNCIL_API_KEY: API key
//
// Communicates via stdin/stdout JSON-RPC 2.0.
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

	"github.com/simon/mneme/pkg/tools"
)

// ── Extension protocol types ────────────────────────────────────────────────

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

type callToolParams struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type callToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ── Manifest ────────────────────────────────────────────────────────────────

type manifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	ProtocolMin int      `json:"protocol_min"`
}

var extManifest = manifest{
	Name:        "model-council",
	Version:     "0.1.0",
	Description: "Multi-model deliberation: query multiple LLMs and aggregate their responses",
	Tools:       []string{"model_council_deliberate"},
	ProtocolMin: 1,
}

type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Permission  string                 `json:"permission"`
	HasEffects  bool                   `json:"has_effects"`
}

var toolDefs = []toolDef{
	{
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
		Permission: tools.PermReadOnly.String(),
		HasEffects: false,
	},
}

// ── Main ──────────────────────────────────────────────────────────────────

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("model-council extension starting")
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
		result, _ := json.Marshal(map[string]interface{}{"tools": toolDefs})
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
		case "model_council_deliberate":
			result = deliberate(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown tool: %s", params.Name)}
		}
		res, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown method: %s", req.Method)}}
	}
}

// ── Council logic ──────────────────────────────────────────────────────────

type councilMember struct {
	Model string `json:"model"`
	Reply string `json:"reply"`
	Error string `json:"error,omitempty"`
}

func deliberate(ctx context.Context, args map[string]interface{}) callToolResult {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return callToolResult{Error: "prompt is required"}
	}

	baseURL := os.Getenv("COUNCIL_BASE_URL")
	apiKey := os.Getenv("COUNCIL_API_KEY")
	modelsStr := os.Getenv("COUNCIL_MODELS")

	if baseURL == "" || apiKey == "" || modelsStr == "" {
		return callToolResult{Error: "not configured. Set COUNCIL_BASE_URL, COUNCIL_API_KEY, and COUNCIL_MODELS env vars."}
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
	return callToolResult{Success: true, Output: string(out)}
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
