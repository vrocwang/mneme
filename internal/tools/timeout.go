package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	pkgtools "github.com/simon/mneme/pkg/tools"
)

const (
	// DefaultTimeoutSecs is the default tool execution timeout in seconds. It
	// aliases pkg/tools.DefaultTimeoutSecs, the single source of truth, so this
	// runtime fallback and the config default cannot drift apart.
	DefaultTimeoutSecs = pkgtools.DefaultTimeoutSecs

	// MinTimeoutSecs is the minimum allowed timeout.
	MinTimeoutSecs = 1

	// MaxTimeoutSecs is the maximum allowed timeout.
	MaxTimeoutSecs = 3600

	// TimeoutEnvVar is the operator override env var. When set, it always wins.
	TimeoutEnvVar = "MNEME_TOOL_TIMEOUT_SECS"
)

var (
	// globalTimeoutSecs stores the current tool timeout in seconds.
	globalTimeoutSecs atomic.Int64

	// envOverrideActive is set when the env var provides the current value.
	envOverrideActive atomic.Bool
)

func init() {
	if envVal := os.Getenv(TimeoutEnvVar); envVal != "" {
		if secs, err := strconv.Atoi(envVal); err == nil {
			secs = clampTimeout(secs)
			globalTimeoutSecs.Store(int64(secs))
			envOverrideActive.Store(true)
			slog.Default().Info("[tools:timeout] using env override",
				"env", TimeoutEnvVar, "seconds", secs)
			return
		}
	}
	globalTimeoutSecs.Store(DefaultTimeoutSecs)
}

// ToolTimeoutSecs returns the current global tool timeout in seconds.
func ToolTimeoutSecs() int {
	return int(globalTimeoutSecs.Load())
}

// ToolTimeoutDuration returns the current timeout as a time.Duration.
func ToolTimeoutDuration() time.Duration {
	return time.Duration(ToolTimeoutSecs()) * time.Second
}

// SetToolTimeoutSecs pushes a new timeout value from config.
// Logged; ignored when the env override is active.
func SetToolTimeoutSecs(configSecs int) {
	configSecs = clampTimeout(configSecs)
	if envOverrideActive.Load() {
		slog.Default().Info("[tools:timeout] config push ignored (env override active)",
			"config_secs", configSecs, "active_secs", ToolTimeoutSecs())
		return
	}
	old := globalTimeoutSecs.Swap(int64(configSecs))
	slog.Default().Info("[tools:timeout] timeout updated",
		"old_secs", old, "new_secs", configSecs, "source", "config")
}

// ExecuteWithTimeout runs a tool with its own declared budget, falling back to
// the global tool timeout and to an explicit per-call override (highest
// precedence). A tool's declaration cannot escape MinTimeoutSecs..MaxTimeoutSecs.
func ExecuteWithTimeout(ctx context.Context, tool Tool, args map[string]interface{}, opts *ToolCallOptions) Result {
	timeout := ToolTimeoutDuration()
	if hinted, ok := tool.(TimeoutHintTool); ok {
		if hint := hinted.TimeoutHint(); hint > 0 {
			timeout = time.Duration(clampTimeout(int(hint.Seconds()))) * time.Second
		}
	}
	if opts != nil && opts.TimeoutOverride > 0 {
		timeout = time.Duration(clampTimeout(opts.TimeoutOverride)) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return executeWithContext(ctx, tool, args, timeout)
}

func executeWithContext(ctx context.Context, tool Tool, args map[string]interface{}, timeout time.Duration) Result {
	resultCh := make(chan Result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				select {
				case resultCh <- Result{Success: false, Error: fmt.Sprintf("tool panic: %v", r)}:
				case <-ctx.Done():
				}
			}
		}()
		select {
		case resultCh <- tool.Execute(ctx, args):
		case <-ctx.Done():
		}
	}()

	select {
	case result := <-resultCh:
		return result
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			// Report the configured budget: measuring time.Until(deadline) here
			// happens after expiry and produced negative durations in the message
			// the model and the user see.
			return Result{Error: "tool execution timed out after " + timeout.String()}
		}
		return Result{Error: fmt.Sprintf("tool execution cancelled: %v", ctx.Err())}
	}
}

func clampTimeout(secs int) int {
	if secs < MinTimeoutSecs {
		return MinTimeoutSecs
	}
	if secs > MaxTimeoutSecs {
		return MaxTimeoutSecs
	}
	return secs
}
