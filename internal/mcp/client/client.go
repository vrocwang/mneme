package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Tool represents a tool discovered from an MCP server.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"inputSchema"`
}

// Result is the result of an MCP tool call.
type Result struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// Client is an MCP JSON-RPC client.
type Client struct {
	name    string
	cmd     *exec.Cmd // for stdio transport
	stdin   io.WriteCloser
	stdout  *bufio.Scanner
	httpURL string
	http    *http.Client

	mu     sync.Mutex // serializes stdio request/response pairs
	nextID int64
}

// NewStdio creates an MCP client that communicates over stdio.
func NewStdio(name string, command string, args ...string) (*Client, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp start %s: %w", name, err)
	}

	// Continuously drain stderr into a ring buffer for diagnostics.
	// Using io.ReadAll(LimitReader(...)) would cap at 4 KiB and risk a
	// deadlock if the subprocess fills the pipe buffer.
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
		var ring [4096]byte
		var n int
		for sc.Scan() {
			line := sc.Bytes()
			copy(ring[n:], line)
			n = (n + len(line)) % len(ring)
		}
		if n > 0 {
			slog.Debug("mcp server stderr", "name", name, "output", string(ring[:n]))
		}
	}()

	stdoutScanner := bufio.NewScanner(stdout)
	stdoutScanner.Buffer(make([]byte, 0, 1024*1024), 2*1024*1024)

	c := &Client{
		name:   name,
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdoutScanner,
	}

	// Initialize — send request and read the initialize response to complete
	// the MCP handshake.
	if err := c.callStdio(context.Background(), "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
	}, nil); err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("mcp init %s: %w", name, err)
	}

	return c, nil
}

// NewHTTP creates an MCP client over HTTP transport.
// The caller must call Initialize() before ListTools or CallTool.
func NewHTTP(name, url string) *Client {
	return &Client{
		name:    name,
		httpURL: strings.TrimRight(url, "/"),
		http:    &http.Client{},
	}
}

// Initialize performs the MCP handshake. For stdio clients this is called
// automatically by NewStdio. For HTTP clients the caller must invoke it
// explicitly before ListTools or CallTool. Safe to call multiple times
// (subsequent calls are no-ops).
func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.httpURL != "" {
		// HTTP transport: send initialize request.
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{},
			},
			"id": 0,
		}
		body, err := json.Marshal(initReq)
		if err != nil {
			return fmt.Errorf("mcp init marshal: %w", err)
		}
		req, _ := http.NewRequestWithContext(ctx, "POST", c.httpURL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("mcp init %s: %w", c.name, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("mcp init %s: HTTP %d", c.name, resp.StatusCode)
		}
	}
	// stdio clients are already initialized in NewStdio; no-op here.
	return nil
}

// Close shuts down the MCP client. For stdio subprocesses, it closes stdin,
// waits up to 2 seconds for graceful exit, then force-kills.
func (c *Client) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		c.stdin.Close()
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()
		select {
		case err := <-done:
			return err
		case <-time.After(2 * time.Second):
			c.cmd.Process.Kill()
			<-done
			return fmt.Errorf("mcp client %s: killed after timeout", c.name)
		}
	}
	return nil
}

// ListTools discovers tools from the MCP server and sanitizes their
// descriptions for LLM consumption.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var resp struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", nil, &resp); err != nil {
		return nil, err
	}
	return SanitizeTools(resp.Tools), nil
}

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (*Result, error) {
	var resp Result
	if err := c.call(ctx, "tools/call", map[string]interface{}{
		"name":      name,
		"arguments": args,
	}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) call(ctx context.Context, method string, params, result interface{}) error {
	if c.httpURL != "" {
		return c.callHTTP(ctx, method, params, result)
	}
	return c.callStdio(ctx, method, params, result)
}

func (c *Client) callHTTP(ctx context.Context, method string, params, result interface{}) error {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal mcp request: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", c.httpURL, bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return err
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("mcp error: %s", rpcResp.Error.Message)
	}
	if result != nil {
		return json.Unmarshal(rpcResp.Result, result)
	}
	return nil
}

func (c *Client) callStdio(ctx context.Context, method string, params, result interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID
	c.nextID++

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      id,
	}
	jsonReq, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	if _, err := fmt.Fprintf(c.stdin, "%s\n", jsonReq); err != nil {
		return fmt.Errorf("write stdin: %w", err)
	}

	return c.readResponse(result)
}

// readResponse reads a single JSON-RPC response line from stdout and unmarshals it.
func (c *Client) readResponse(result interface{}) error {
	if !c.stdout.Scan() {
		if err := c.stdout.Err(); err != nil {
			return fmt.Errorf("read stdout: %w", err)
		}
		return fmt.Errorf("mcp server closed stdout unexpectedly")
	}

	line := c.stdout.Text()
	if line == "" {
		return fmt.Errorf("empty response from mcp server")
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &rpcResp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("mcp error [%d]: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if result != nil && len(rpcResp.Result) > 0 {
		return json.Unmarshal(rpcResp.Result, result)
	}
	return nil
}
