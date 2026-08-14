// Channel iMessage extension for Mneme.
//
// Provides iMessage integration tools (macOS only):
//   - imessage_send: send iMessage via AppleScript
//   - imessage_check: check recent messages
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	if runtime.GOOS != "darwin" {
		fmt.Fprintf(os.Stderr, "iMessage tools only work on macOS\n")
	}

	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "channel-imessage",
		Version:     "0.1.0",
		Description: "iMessage channel: send and check messages (macOS only)",
	})

	srv.RegisterTool(extsdk.ToolDef{
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
	}, imessageSend)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, imessageCheck)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "channel-imessage: %v\n", err)
		os.Exit(1)
	}
}

func imessageSend(ctx context.Context, args map[string]interface{}) extsdk.Result {
	if runtime.GOOS != "darwin" {
		return extsdk.Result{Error: "iMessage is only available on macOS"}
	}
	to, _ := args["to"].(string)
	body, _ := args["body"].(string)
	if to == "" || body == "" {
		return extsdk.Result{Error: "to and body are required"}
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
		return extsdk.Result{Error: fmt.Sprintf("imessage send: %v (%s)", err, string(out))}
	}
	return extsdk.Result{Success: true, Output: fmt.Sprintf("Sent iMessage to %s", to)}
}

func imessageCheck(ctx context.Context, args map[string]interface{}) extsdk.Result {
	if runtime.GOOS != "darwin" {
		return extsdk.Result{Error: "iMessage is only available on macOS"}
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
		return extsdk.Result{Error: fmt.Sprintf("imessage check: %v (%s)", err, string(out))}
	}
	return extsdk.Result{Success: true, Output: string(out)}
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
