// Channel IRC extension for Mneme.
//
// Provides IRC integration tools:
//   - irc_connect: connect to an IRC server
//   - irc_send: send a message to a channel or user
//   - irc_join: join a channel
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

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
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "channel-irc",
		Version:     "0.1.0",
		Description: "IRC channel: connect, send messages, join channels",
	})

	srv.RegisterTool(extsdk.ToolDef{
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
	}, ircConnect)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, ircSend)

	srv.RegisterTool(extsdk.ToolDef{
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
	}, ircJoin)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "channel-irc: %v\n", err)
		os.Exit(1)
	}
}

func ircConnect(ctx context.Context, args map[string]interface{}) extsdk.Result {
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
		return extsdk.Result{Error: fmt.Sprintf("connect: %v", err)}
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
			return extsdk.Result{Error: fmt.Sprintf("read welcome: %v", err)}
		}
		if strings.Contains(line, " 001 ") {
			return extsdk.Result{Success: true, Output: fmt.Sprintf("Connected to %s as %s\nID: %s", host, nick, id)}
		}
		if strings.Contains(line, " 433 ") {
			// Remove from map before closing to avoid leaked entry
			connectionsMu.Lock()
			delete(connections, id)
			connectionsMu.Unlock()
			conn.Close()
			return extsdk.Result{Error: fmt.Sprintf("Nickname %s is already in use", nick)}
		}
	}
	return extsdk.Result{Success: true, Output: fmt.Sprintf("Connected to %s as %s\nID: %s", host, nick, id)}
}

func ircSend(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = ctx
	connID, _ := args["connId"].(string)
	target, _ := args["target"].(string)
	message, _ := args["message"].(string)
	if connID == "" || target == "" || message == "" {
		return extsdk.Result{Error: "connId, target, and message are required"}
	}

	target = sanitizeIRC(target)
	message = sanitizeIRC(message)

	connectionsMu.Lock()
	irc, ok := connections[connID]
	connectionsMu.Unlock()
	if !ok {
		return extsdk.Result{Error: fmt.Sprintf("connection not found: %s", connID)}
	}

	irc.mu.Lock()
	defer irc.mu.Unlock()
	fmt.Fprintf(irc.Conn, "PRIVMSG %s :%s\r\n", target, message)
	return extsdk.Result{Success: true, Output: fmt.Sprintf("Sent to %s: %s", target, message)}
}

func ircJoin(ctx context.Context, args map[string]interface{}) extsdk.Result {
	_ = ctx
	connID, _ := args["connId"].(string)
	channel, _ := args["channel"].(string)
	if connID == "" || channel == "" {
		return extsdk.Result{Error: "connId and channel are required"}
	}
	if !strings.HasPrefix(channel, "#") {
		channel = "#" + channel
	}

	channel = sanitizeIRC(channel)

	connectionsMu.Lock()
	irc, ok := connections[connID]
	connectionsMu.Unlock()
	if !ok {
		return extsdk.Result{Error: fmt.Sprintf("connection not found: %s", connID)}
	}

	irc.mu.Lock()
	defer irc.mu.Unlock()
	fmt.Fprintf(irc.Conn, "JOIN %s\r\n", channel)
	return extsdk.Result{Success: true, Output: fmt.Sprintf("Joining %s", channel)}
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
