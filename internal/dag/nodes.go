package dag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// NodeExecutor executes a single node with the given inputs and returns
// outputs. Each node type has its own execution strategy.
type NodeExecutor struct {
	// AgentRunner is invoked for KindAgent nodes. When nil, agent nodes fail.
	AgentRunner func(ctx context.Context, prompt string) (string, error)
}

// Execute runs a single node and returns its output items.
func (e *NodeExecutor) Execute(ctx context.Context, node Node, input *NodeInput) (*NodeOutput, error) {
	switch node.Kind {
	case KindHTTPRequest:
		return e.execHTTPRequest(ctx, node, input)
	case KindCondition:
		return e.execCondition(ctx, node, input)
	case KindCode:
		return e.execCode(ctx, node, input)
	case KindAgent:
		return e.execAgent(ctx, node, input)
	case KindTransform:
		return e.execTransform(ctx, node, input)
	case KindTriggerManual, KindTriggerCron, KindTriggerWebhook:
		// Triggers pass through — they emit their input as output.
		return input.toOutput(), nil
	default:
		return nil, fmt.Errorf("dag: unknown node kind %q", node.Kind)
	}
}

// ── HTTP Request ──────────────────────────────────────────────────────

func (e *NodeExecutor) execHTTPRequest(ctx context.Context, node Node, input *NodeInput) (*NodeOutput, error) {
	method, _ := node.Config["method"].(string)
	if method == "" {
		method = "GET"
	}
	url, _ := node.Config["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("http_request node %q: url is required", node.ID)
	}

	// Resolve template variables from input items.
	url = resolveTemplate(url, input.firstItem())
	bodyStr, _ := node.Config["body"].(string)
	bodyStr = resolveTemplate(bodyStr, input.firstItem())

	var body io.Reader
	if bodyStr != "" {
		body = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("http_request node %q: %w", node.ID, err)
	}

	// Headers.
	if headers, ok := node.Config["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprint(v))
		}
	}
	if bodyStr != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http_request node %q: %w", node.ID, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB cap

	item := map[string]interface{}{
		"status":  resp.StatusCode,
		"body":    string(respBody),
		"headers": resp.Header,
		"url":     url,
		"method":  method,
	}
	return &NodeOutput{Items: []map[string]interface{}{item}}, nil
}

// ── Condition ─────────────────────────────────────────────────────────

func (e *NodeExecutor) execCondition(_ context.Context, node Node, input *NodeInput) (*NodeOutput, error) {
	field, _ := node.Config["field"].(string)
	op, _ := node.Config["op"].(string) // eq, neq, gt, lt, contains, empty, not_empty
	value := node.Config["value"]

	item := input.firstItem()
	result := evaluateCondition(item, field, op, value)

	port := "false"
	if result {
		port = "true"
	}

	return &NodeOutput{
		Items: []map[string]interface{}{mergeItem(item, map[string]interface{}{
			"_condition_result": result,
			"_condition_port":   port,
		})},
	}, nil
}

func evaluateCondition(item map[string]interface{}, field, op string, expected interface{}) bool {
	actual, ok := item[field]
	if !ok {
		// Try nested lookup.
		actual = nestedGet(item, field)
	}

	switch op {
	case "empty":
		return actual == nil || actual == "" || actual == 0
	case "not_empty":
		return actual != nil && actual != "" && actual != 0
	case "contains":
		actualStr := fmt.Sprint(actual)
		expectedStr := fmt.Sprint(expected)
		return strings.Contains(actualStr, expectedStr)
	}

	if actual == nil {
		return false
	}

	a := fmt.Sprint(actual)
	e := fmt.Sprint(expected)

	switch op {
	case "eq", "":
		// Try numeric comparison first.
		if af, err := strconv.ParseFloat(a, 64); err == nil {
			if ef, err := strconv.ParseFloat(e, 64); err == nil {
				return af == ef
			}
		}
		return a == e
	case "neq":
		return a != e
	case "gt":
		af, _ := strconv.ParseFloat(a, 64)
		ef, _ := strconv.ParseFloat(e, 64)
		return af > ef
	case "lt":
		af, _ := strconv.ParseFloat(a, 64)
		ef, _ := strconv.ParseFloat(e, 64)
		return af < ef
	default:
		return false
	}
}

func nestedGet(item map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	var current interface{} = item
	for _, p := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = m[p]
	}
	return current
}

// ── Code ──────────────────────────────────────────────────────────────

func (e *NodeExecutor) execCode(ctx context.Context, node Node, input *NodeInput) (*NodeOutput, error) {
	language, _ := node.Config["language"].(string)
	source, _ := node.Config["source"].(string)
	if source == "" {
		return nil, fmt.Errorf("code node %q: source is required", node.ID)
	}

	var cmd *exec.Cmd
	switch language {
	case "python", "python3":
		cmd = exec.CommandContext(ctx, "python3", "-c", source)
	case "javascript", "js", "node":
		cmd = exec.CommandContext(ctx, "node", "-e", source)
	case "shell", "bash", "sh":
		cmd = exec.CommandContext(ctx, "bash", "-c", source)
	default:
		return nil, fmt.Errorf("code node %q: unsupported language %q", node.ID, language)
	}

	// Pass input as JSON via stdin.
	inputJSON := toJSON(input.firstItem())
	cmd.Stdin = strings.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())
	if err != nil {
		errDetail := stderr.String()
		if errDetail == "" {
			errDetail = err.Error()
		}
		return nil, fmt.Errorf("code node %q failed: %s", node.ID, errDetail)
	}

	item := map[string]interface{}{
		"stdout": output,
		"stderr": stderr.String(),
	}
	// If output looks like JSON, parse it into the item.
	if parsed, ok := tryParseJSON(output); ok {
		if m, ok := parsed.(map[string]interface{}); ok {
			return &NodeOutput{Items: []map[string]interface{}{m}}, nil
		}
	}

	return &NodeOutput{Items: []map[string]interface{}{item}}, nil
}

// ── Agent ─────────────────────────────────────────────────────────────

func (e *NodeExecutor) execAgent(ctx context.Context, node Node, input *NodeInput) (*NodeOutput, error) {
	if e.AgentRunner == nil {
		return nil, fmt.Errorf("agent node %q: no AgentRunner configured", node.ID)
	}

	prompt, _ := node.Config["prompt"].(string)
	if prompt == "" {
		return nil, fmt.Errorf("agent node %q: prompt is required", node.ID)
	}

	// Resolve template variables.
	prompt = resolveTemplate(prompt, input.firstItem())

	result, err := e.AgentRunner(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("agent node %q: %w", node.ID, err)
	}

	item := map[string]interface{}{
		"response": result,
		"prompt":   prompt,
	}
	return &NodeOutput{Items: []map[string]interface{}{item}}, nil
}

// ── Transform ─────────────────────────────────────────────────────────

func (e *NodeExecutor) execTransform(_ context.Context, node Node, input *NodeInput) (*NodeOutput, error) {
	set, ok := node.Config["set"].(map[string]interface{})
	if !ok {
		// Passthrough.
		return input.toOutput(), nil
	}

	item := input.firstItem()
	out := mergeItem(item, nil)
	for k, v := range set {
		if str, ok := v.(string); ok && strings.HasPrefix(str, "=") {
			// Expression: =item.field
			expr := strings.TrimPrefix(str, "=")
			if val := nestedGet(item, expr); val != nil {
				out[k] = val
			}
		} else {
			out[k] = v
		}
	}
	return &NodeOutput{Items: []map[string]interface{}{out}}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────

func (in *NodeInput) firstItem() map[string]interface{} {
	if len(in.Items) == 0 {
		return map[string]interface{}{}
	}
	return in.Items[0]
}

func (in *NodeInput) toOutput() *NodeOutput {
	return &NodeOutput{Items: in.Items}
}

func mergeItem(base, overlay map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

func resolveTemplate(tmpl string, item map[string]interface{}) string {
	if item == nil || !strings.Contains(tmpl, "{{") {
		return tmpl
	}
	for k, v := range item {
		placeholder := "{{" + k + "}}"
		tmpl = strings.ReplaceAll(tmpl, placeholder, fmt.Sprint(v))
	}
	return tmpl
}

func toJSON(v interface{}) string {
	b, _ := jsonMarshal(v)
	return string(b)
}

func jsonMarshal(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func tryParseJSON(s string) (interface{}, bool) {
	s = strings.TrimSpace(s)
	if len(s) == 0 || (s[0] != '{' && s[0] != '[') {
		return nil, false
	}
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, false
	}
	return v, true
}
