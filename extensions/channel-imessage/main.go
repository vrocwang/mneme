// Channel iMessage extension for Mneme.
//
// Provides iMessage integration tools (macOS only):
//   - imessage_send: send iMessage via AppleScript
//   - imessage_check: check recent messages
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
	"os"
	"os/exec"
	"runtime"
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
	Name:        "channel-imessage",
	Version:     "0.1.0",
	Description: "iMessage channel: send and check messages (macOS only)",
	Tools:       []string{"imessage_send", "imessage_check"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "imessage_send",
		Description: "Send an iMessage to a phone number or email. macOS only.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"to":   map[string]interface{}{"type": "string", "description": "Recipient phone number or email"},
				"body": map[string]interface{}{"type": "string", "description": "Message body"},
			},
			"required": []string{"to", "body"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "imessage_check",
		Description: "Check recent iMessages. macOS only.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"limit":      map[string]interface{}{"type": "number", "description": "Max messages to return (default 10)"},
				"sender":     map[string]interface{}{"type": "string", "description": "Filter by sender"},
				"unreadOnly": map[string]interface{}{"type": "boolean", "description": "Only show unread messages"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	},
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("channel-imessage extension starting")
	if runtime.GOOS != "darwin" {
		log.Warn("iMessage tools only work on macOS")
	}
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
		type listResult struct {
			Tools []toolDef `json:"tools"`
		}
		result, _ := json.Marshal(listResult{Tools: toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		type listAgentsResult struct {
			Agents []interface{} `json:"agents"`
		}
		result, _ := json.Marshal(listAgentsResult{Agents: []interface{}{}})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "imessage_send":
			result = imessageSend(ctx, params.Args)
		case "imessage_check":
			result = imessageCheck(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func imessageSend(ctx context.Context, args map[string]interface{}) callToolResult {
	if runtime.GOOS != "darwin" {
		return callToolResult{Error: "iMessage is only available on macOS"}
	}
	to, _ := args["to"].(string)
	body, _ := args["body"].(string)
	if to == "" || body == "" {
		return callToolResult{Error: "to and body are required"}
	}
	escapedTo := strings.ReplaceAll(to, `\`, `\\`)
	escapedTo = strings.ReplaceAll(escapedTo, `"`, `\"`)
	escapedBody := strings.ReplaceAll(body, `\`, `\\`)
	escapedBody = strings.ReplaceAll(escapedBody, `"`, `\"`)
	script := fmt.Sprintf(`tell application "Messages"
    set targetBuddy to buddy "%s"
    send "%s" to targetBuddy
end tell`, escapedTo, escapedBody)
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("imessage send: %v (%s)", err, string(out))}
	}
	return callToolResult{Success: true, Output: fmt.Sprintf("Sent iMessage to %s", to)}
}

func imessageCheck(ctx context.Context, args map[string]interface{}) callToolResult {
	if runtime.GOOS != "darwin" {
		return callToolResult{Error: "iMessage is only available on macOS"}
	}
	limit := 10
	if l, ok := getInt(args, "limit"); ok && l > 0 {
		limit = l
	}
	sender, _ := args["sender"].(string)
	unreadOnly, _ := args["unreadOnly"].(bool)

	escapedSender := strings.ReplaceAll(sender, `\`, `\\`)
	escapedSender = strings.ReplaceAll(escapedSender, `"`, `\"`)

	script := fmt.Sprintf(`tell application "Messages"
    set msgList to ""
    set msgCount to 0
    repeat with c in (get every chat)
        repeat with m in (get messages of c)
            if msgCount >= %d then exit repeat
            set msgText to (content of m) as string
            if msgText is not "" then
                set msgList to msgList & (sender of m as string) & ": " & msgText & linefeed
                set msgCount to msgCount + 1
            end if
        end repeat
    end repeat
    return msgList
end tell`, limit)
	if sender != "" {
		script = strings.Replace(script, `(content of m) as string`,
			fmt.Sprintf(`(content of m) as string
            if (sender of m as string) contains "%s" then`, escapedSender), 1)
	}
	_ = unreadOnly // Messages.app doesn't expose read/unread easily

	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("imessage check: %v (%s)", err, string(out))}
	}
	return callToolResult{Success: true, Output: string(out)}
}

func getInt(args map[string]interface{}, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}
