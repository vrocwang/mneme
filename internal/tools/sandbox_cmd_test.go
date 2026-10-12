package tools

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"strings"
	"testing"

	"github.com/simon/mneme/internal/security/sandbox"
)

// erroringBackend is an available sandbox backend whose wrapping always fails,
// which is the fail-open path Sandbox.Command must make visible.
type erroringBackend struct{ err error }

func (b erroringBackend) Name() string    { return "erroring" }
func (b erroringBackend) Available() bool { return true }
func (b erroringBackend) WrapCommand(context.Context, string, string, ...string) (*exec.Cmd, error) {
	return nil, b.err
}

// TestSandboxCommandLogsUnsandboxedFallback guards the fail-open path: when a
// non-mandatory sandbox backend fails to wrap the command, the command still
// runs, but a warning naming the command and the wrap error must be logged so
// the skipped sandboxing is not invisible.
func TestSandboxCommandLogsUnsandboxedFallback(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	wrapErr := errors.New("bwrap not usable")
	sb := newSandbox(erroringBackend{err: wrapErr}, false)

	cmd, err := sb.Command(context.Background(), t.TempDir(), "echo", "hi")
	if err != nil {
		t.Fatalf("expected fail-open fallback, got error: %v", err)
	}
	if cmd == nil {
		t.Fatal("expected a command on the fail-open fallback, got nil")
	}

	out := buf.String()
	for _, want := range []string{"unsandboxed", "echo", "bwrap not usable"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning log %q does not mention %q", out, want)
		}
	}
}

// TestSandboxCommandRequiredStaysFailClosed verifies the fix did not weaken the
// mandatory mode: a required sandbox with no usable backend must still refuse.
func TestSandboxCommandRequiredStaysFailClosed(t *testing.T) {
	sb := newSandbox(sandbox.NoopBackend(), true)
	cmd, err := sb.Command(context.Background(), t.TempDir(), "echo", "hi")
	if err == nil {
		t.Fatal("expected an error when sandboxing is required but unavailable")
	}
	if cmd != nil {
		t.Fatalf("expected no command on the fail-closed path, got %v", cmd)
	}
}
