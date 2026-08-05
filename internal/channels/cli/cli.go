package cli

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/simon/mneme/internal/channels"
)

// CLI channel reads from stdin and writes to stdout.
type CLI struct {
	events   chan channels.Message
	cancel   context.CancelFunc
	stopOnce sync.Once
}

func New() *CLI {
	return &CLI{events: make(chan channels.Message, 64)}
}

func (c *CLI) Name() string { return "cli" }

func (c *CLI) Start(ctx context.Context) error {
	ctx, c.cancel = context.WithCancel(ctx)
	go c.readLoop(ctx)
	fmt.Fprintln(os.Stderr, "[cli] channel started — type messages, Ctrl+D to stop")
	return nil
}

func (c *CLI) Stop() error {
	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		close(c.events)
	})
	return nil
}

func (c *CLI) Send(ctx context.Context, msg channels.Message) error {
	fmt.Fprintf(os.Stdout, "\n[%s] %s\n", msg.Channel, msg.Content)
	return nil
}

func (c *CLI) Events() <-chan channels.Message {
	return c.events
}

func (c *CLI) readLoop(ctx context.Context) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		if text == "/exit" || text == "/quit" {
			return
		}
		sanitized, blocked := channels.SanitizeInbound(text, slog.Default())
		if blocked {
			fmt.Fprintln(os.Stderr, "[cli] message blocked by prompt injection guard")
			continue
		}
		msg := channels.Message{
			ID:      fmt.Sprintf("cli-%d", time.Now().UnixMicro()),
			Channel: "cli",
			Content: sanitized,
		}
		select {
		case c.events <- msg:
		case <-ctx.Done():
			return
		}
	}
}
