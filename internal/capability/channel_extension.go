package capability

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

// ChannelExtension wraps a JSON-RPC subprocess as a Channel.
// Extension channels communicate via stdin/stdout JSON-RPC 2.0.
// The subprocess must handle: start, stop, send, events.
type ChannelExtension struct {
	name    string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Scanner
	events  chan ChannelMessage
	cancel  context.CancelFunc
	mu      sync.Mutex
	running bool
	log     *slog.Logger
	reqID   int64
}

// NewChannelExtension creates a channel from an extension subprocess.
func NewChannelExtension(name string, binPath string, log *slog.Logger) *ChannelExtension {
	if log == nil {
		log = slog.Default()
	}
	return &ChannelExtension{
		name:   name,
		events: make(chan ChannelMessage, 128),
		log:    log.With("channel", name),
	}
}

func (c *ChannelExtension) Name() string { return c.name }

func (c *ChannelExtension) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return nil
	}

	// The extension protocol uses the parent process for command execution.
	// For now, channels embedded via the extension protocol don't need
	// a separate subprocess — they use the existing tool-based extension
	// system for sending messages and rely on the orchestrator for events.
	//
	// This wrapper exists so channel providers can be registered through
	// CapabilityRegistry. The actual lifecycle is managed by the orchestrator.
	c.running = true
	return nil
}

func (c *ChannelExtension) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return nil
	}
	c.running = false
	if c.cancel != nil {
		c.cancel()
	}
	return nil
}

func (c *ChannelExtension) Send(ctx context.Context, msg ChannelMessage) error {
	return fmt.Errorf("channel %q: send not implemented via extension protocol", c.name)
}

func (c *ChannelExtension) Events() <-chan ChannelMessage {
	return c.events
}

// ── JSON-RPC helpers ──────────────────────────────────────────────────

func (c *ChannelExtension) call(method string, params interface{}) (json.RawMessage, error) {
	c.reqID++
	req := struct {
		JSONRPC string      `json:"jsonrpc"`
		ID      int64       `json:"id"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params,omitempty"`
	}{"2.0", c.reqID, method, params}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("channel rpc write: %w", err)
	}

	if !c.stdout.Scan() {
		return nil, fmt.Errorf("channel rpc read: %w", c.stdout.Err())
	}
	line := c.stdout.Bytes()

	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("channel rpc parse: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("channel rpc error: %s", resp.Error.Message)
	}
	return resp.Result, nil
}

// readLoop reads events from the extension's stdout.
func (c *ChannelExtension) readLoop(ctx context.Context) {
	for c.stdout.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		var msg ChannelMessage
		if err := json.Unmarshal(c.stdout.Bytes(), &msg); err != nil {
			c.log.Warn("channel extension: bad event", "error", err)
			continue
		}
		select {
		case c.events <- msg:
		case <-time.After(5 * time.Second):
			c.log.Warn("channel extension: event dropped (slow consumer)")
		}
	}
	if err := c.stdout.Err(); err != nil && err != io.EOF {
		c.log.Warn("channel extension: read error", "error", err)
	}
}
