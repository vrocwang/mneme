// Channel IRC extension for Mneme.
//
// Provides IRC integration tools:
//   - irc_connect: connect to an IRC server
//   - irc_send: send a message to a channel or user
//   - irc_join: join a channel
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
	"net"
	"os"
	"strings"
	"sync"
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
	Name:        "channel-irc",
	Version:     "0.1.0",
	Description: "IRC channel: connect, send messages, join channels",
	Tools:       []string{"irc_connect", "irc_send", "irc_join"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "irc_connect",
		Description: "Connect to an IRC server. Returns a connection ID for use with irc_send and irc_join.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"host":     map[string]interface{}{"type": "string", "description": "IRC server hostname"},
				"port":     map[string]interface{}{"type": "number", "description": "Port (default 6667)"},
				"nick":     map[string]interface{}{"type": "string", "description": "Nickname"},
				"user":     map[string]interface{}{"type": "string", "description": "Username (default: same as nick)"},
				"realName": map[string]interface{}{"type": "string", "description": "Real name"},
				"password": map[string]interface{}{"type": "string", "description": "Server password (optional)"},
				"tls":      map[string]interface{}{"type": "boolean", "description": "Use TLS (default false)"},
			},
			"required": []string{"host", "nick"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "irc_send",
		Description: "Send a message to an IRC channel or user",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connId":  map[string]interface{}{"type": "string", "description": "Connection ID from irc_connect"},
				"target":  map[string]interface{}{"type": "string", "description": "Channel (#channel) or user nickname"},
				"message": map[string]interface{}{"type": "string", "description": "Message to send"},
			},
			"required": []string{"connId", "target", "message"},
		},
		Permission: "execute",
		HasEffects: true,
	},
	{
		Name:        "irc_join",
		Description: "Join an IRC channel",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connId":  map[string]interface{}{"type": "string", "description": "Connection ID from irc_connect"},
				"channel": map[string]interface{}{"type": "string", "description": "Channel name (e.g. #mneme)"},
			},
			"required": []string{"connId", "channel"},
		},
		Permission: "execute",
		HasEffects: true,
	},
}

type ircConn struct {
	ID     string
	Conn   net.Conn
	Nick   string
	mu     sync.Mutex
	reader *bufio.Reader
}

var (
	connections   = make(map[string]*ircConn)
	connectionsMu sync.Mutex
	connSeq       int64
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("channel-irc extension starting")
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
		case "irc_connect":
			result = ircConnect(ctx, params.Args)
		case "irc_send":
			result = ircSend(params.Args)
		case "irc_join":
			result = ircJoin(params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func ircConnect(ctx context.Context, args map[string]interface{}) callToolResult {
	host, _ := args["host"].(string)
	nick, _ := args["nick"].(string)
	port := 6667
	if p, ok := getInt(args, "port"); ok && p > 0 {
		port = p
	}
	user, _ := args["user"].(string)
	if user == "" {
		user = nick
	}
	realName, _ := args["realName"].(string)
	if realName == "" {
		realName = nick
	}
	password, _ := args["password"].(string)
	tlsFlag, _ := args["tls"].(bool)

	// Strip IRC protocol injection characters
	host = sanitizeIRC(host)
	nick = sanitizeIRC(nick)
	user = sanitizeIRC(user)
	realName = sanitizeIRC(realName)
	password = sanitizeIRC(password)

	_ = tlsFlag // reserved for future TLS support

	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("connect: %v", err)}
	}

	connectionsMu.Lock()
	connSeq++
	id := fmt.Sprintf("irc_%d", connSeq)
	irc := &ircConn{ID: id, Conn: conn, Nick: nick, reader: bufio.NewReader(conn)}
	connections[id] = irc
	connectionsMu.Unlock()

	// Send registration
	if password != "" {
		fmt.Fprintf(conn, "PASS %s\r\n", password)
	}
	fmt.Fprintf(conn, "NICK %s\r\n", nick)
	fmt.Fprintf(conn, "USER %s 0 * :%s\r\n", user, realName)

	// Read welcome
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	for i := 0; i < 10; i++ {
		line, err := irc.reader.ReadString('\n')
		if err != nil {
			return callToolResult{Error: fmt.Sprintf("read welcome: %v", err)}
		}
		if strings.Contains(line, " 001 ") {
			return callToolResult{Success: true, Output: fmt.Sprintf("Connected to %s as %s\nID: %s", host, nick, id)}
		}
		if strings.Contains(line, " 433 ") {
			// Remove from map before closing to avoid leaked entry
			connectionsMu.Lock()
			delete(connections, id)
			connectionsMu.Unlock()
			conn.Close()
			return callToolResult{Error: fmt.Sprintf("Nickname %s is already in use", nick)}
		}
	}
	return callToolResult{Success: true, Output: fmt.Sprintf("Connected to %s as %s\nID: %s", host, nick, id)}
}

func ircSend(args map[string]interface{}) callToolResult {
	connID, _ := args["connId"].(string)
	target, _ := args["target"].(string)
	message, _ := args["message"].(string)
	if connID == "" || target == "" || message == "" {
		return callToolResult{Error: "connId, target, and message are required"}
	}

	target = sanitizeIRC(target)
	message = sanitizeIRC(message)

	connectionsMu.Lock()
	irc, ok := connections[connID]
	connectionsMu.Unlock()
	if !ok {
		return callToolResult{Error: fmt.Sprintf("connection not found: %s", connID)}
	}

	irc.mu.Lock()
	defer irc.mu.Unlock()
	fmt.Fprintf(irc.Conn, "PRIVMSG %s :%s\r\n", target, message)
	return callToolResult{Success: true, Output: fmt.Sprintf("Sent to %s: %s", target, message)}
}

func ircJoin(args map[string]interface{}) callToolResult {
	connID, _ := args["connId"].(string)
	channel, _ := args["channel"].(string)
	if connID == "" || channel == "" {
		return callToolResult{Error: "connId and channel are required"}
	}
	if !strings.HasPrefix(channel, "#") {
		channel = "#" + channel
	}

	channel = sanitizeIRC(channel)

	connectionsMu.Lock()
	irc, ok := connections[connID]
	connectionsMu.Unlock()
	if !ok {
		return callToolResult{Error: fmt.Sprintf("connection not found: %s", connID)}
	}

	irc.mu.Lock()
	defer irc.mu.Unlock()
	fmt.Fprintf(irc.Conn, "JOIN %s\r\n", channel)
	return callToolResult{Success: true, Output: fmt.Sprintf("Joining %s", channel)}
}

func sanitizeIRC(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
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
