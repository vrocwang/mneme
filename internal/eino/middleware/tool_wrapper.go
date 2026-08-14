package middleware

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/simon/mneme/internal/security"
)

// ToolWrapper wraps a tool.BaseTool with pre-execution (approval gate) and
// post-execution (credential scrubbing, circuit breaker) hooks. It implements
// eino's tool.InvokableTool so it integrates into the native toolkit execution
// pipeline — the agent executes the wrapper, not the raw tool.
type ToolWrapper struct {
	inner tool.BaseTool
	sec   *SecurityMiddleware
	cb    *CircuitBreakerMiddleware
}

// NewToolWrapper creates a tool wrapper. Either sec or cb may be nil (hooks
// are skipped when their controller is absent).
func NewToolWrapper(inner tool.BaseTool, sec *SecurityMiddleware, cb *CircuitBreakerMiddleware) tool.BaseTool {
	return &ToolWrapper{inner: inner, sec: sec, cb: cb}
}

func (w *ToolWrapper) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return w.inner.Info(ctx)
}

// InvokableRun implements tool.InvokableTool. It runs the security check,
// executes the inner tool, scrubs the output, and records the result with the
// circuit breaker.
func (w *ToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (content string, err error) {
	// ── Pre-execute: approval gate ──
	if w.sec != nil {
		if err := w.sec.CheckTool(ctx, w.toolName(), argumentsInJSON); err != nil {
			if w.cb != nil {
				w.cb.RecordFailure(w.toolName(), argumentsInJSON)
			}
			return "", err
		}
		// Mark the context so downstream tools (e.g. the shell tool's tier gate)
		// know this call passed through the approval pipeline and should not
		// conservatively re-block Prompt-class commands.
		ctx = security.WithApprovalHandled(ctx)
	}

	// ── Execute the inner tool ──
	invokable, ok := w.inner.(tool.InvokableTool)
	if !ok {
		// Fall back: call Info to get the tool name for the error, then
		// return a clear diagnostic.
		info, _ := w.inner.Info(ctx)
		name := "unknown"
		if info != nil {
			name = info.Name
		}
		return "", fmt.Errorf("tool %q does not implement InvokableTool", name)
	}

	content, err = invokable.InvokableRun(ctx, argumentsInJSON, opts...)

	// ── Post-execute: circuit breaker ──
	if w.cb != nil {
		if err != nil {
			w.cb.RecordFailure(w.toolName(), argumentsInJSON)
		} else {
			w.cb.RecordSuccess()
		}
	}

	// ── Post-execute: credential scrubbing ──
	if w.sec != nil && err == nil {
		content = w.sec.SanitizeOutput(content)
	}

	return content, err
}

func (w *ToolWrapper) toolName() string {
	// Best-effort: ask the inner tool for its Info.
	info, err := w.inner.Info(context.Background())
	if err != nil || info == nil {
		return ""
	}
	return info.Name
}

// WrapAllTools wraps every tool in the slice with a ToolWrapper. Tools that
// are already wrapped are passed through unchanged.
func WrapAllTools(tools []tool.BaseTool, sec *SecurityMiddleware, cb *CircuitBreakerMiddleware) []tool.BaseTool {
	if len(tools) == 0 {
		return tools
	}
	out := make([]tool.BaseTool, 0, len(tools))
	for _, t := range tools {
		if _, already := t.(*ToolWrapper); already {
			out = append(out, t)
		} else {
			out = append(out, NewToolWrapper(t, sec, cb))
		}
	}
	return out
}
