package cli

import (
	"context"
	"testing"
	"time"

	"github.com/simon/mneme/internal/channels"
)

func TestCLI_Name(t *testing.T) {
	c := New()
	if c.Name() != "cli" {
		t.Errorf("expected cli, got %s", c.Name())
	}
}

func TestCLI_StartStop(t *testing.T) {
	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestCLI_Send(t *testing.T) {
	c := New()
	ctx := context.Background()

	// Send should not error (writes to stdout)
	err := c.Send(ctx, channels.Message{Channel: "cli", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
}
