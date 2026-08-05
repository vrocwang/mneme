package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

const (
	// DefaultTimeoutSecs is the default tool execution timeout in seconds.
	DefaultTimeoutSecs = 120

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

// EnvOverrideActive returns true when the env var is controlling the timeout.
func EnvOverrideActive() bool {
	return envOverrideActive.Load()
}

// ResetTimeoutToDefault clears config push and restores default or env override.
func ResetTimeoutToDefault() {
	envOverrideActive.Store(false)
	if envVal := os.Getenv(TimeoutEnvVar); envVal != "" {
		if secs, err := strconv.Atoi(envVal); err == nil {
			globalTimeoutSecs.Store(int64(clampTimeout(secs)))
			envOverrideActive.Store(true)
			return
		}
	}
	globalTimeoutSecs.Store(DefaultTimeoutSecs)
}

// WithTimeout wraps a tool with a fixed timeout (for callers with specific needs).
func WithTimeout(t Tool, d time.Duration) Tool {
	return &timeoutTool{tool: t, timeout: d}
}

type timeoutTool struct {
	tool    Tool
	timeout time.Duration
}

func (t *timeoutTool) Schema() Schema { return t.tool.Schema() }

func (t *timeoutTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	return executeWithContext(ctx, t.tool, args)
}

// ExecuteWithTimeout runs a tool with the global timeout (or per-call override).
func ExecuteWithTimeout(ctx context.Context, tool Tool, args map[string]interface{}, opts *ToolCallOptions) Result {
	timeout := ToolTimeoutDuration()
	if opts != nil && opts.TimeoutOverride > 0 {
		timeout = time.Duration(clampTimeout(opts.TimeoutOverride)) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return executeWithContext(ctx, tool, args)
}

func executeWithContext(ctx context.Context, tool Tool, args map[string]interface{}) Result {
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
			timeout := "unknown"
			if deadline, ok := ctx.Deadline(); ok {
				timeout = time.Until(deadline).String()
			}
			return Result{Error: "tool execution timed out after " + timeout}
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
