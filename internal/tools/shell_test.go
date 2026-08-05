package tools

import (
	"context"
	"testing"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/security"
)

func TestShell_AllowedRead(t *testing.T) {
	tool := NewShell(t.TempDir(), security.TierFull, config.ToolsShellConfig{}, config.SandboxConfig{})
	result := tool.Execute(context.Background(), map[string]interface{}{
		"command": "echo hello",
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Output != "hello" {
		t.Errorf("expected 'hello', got %q", result.Output)
	}
}

func TestShell_BlockedDestructive(t *testing.T) {
	tool := NewShell(t.TempDir(), security.TierReadOnly, config.ToolsShellConfig{}, config.SandboxConfig{})
	result := tool.Execute(context.Background(), map[string]interface{}{
		"command": "rm -rf /tmp/test",
	})

	if result.Success {
		t.Error("expected block for destructive command in read-only tier")
	}
}

func TestShell_PromptInSupervised(t *testing.T) {
	// In supervised mode, write commands return a policy block when called
	// directly through the tool (raw registry). Approval-gated commands
	// must go through ToolExecutor.PolicyChecker which intercepts Prompt
	// decisions and routes them through the ApprovalGate.
	tool := NewShell(t.TempDir(), security.TierSupervised, config.ToolsShellConfig{}, config.SandboxConfig{})
	result := tool.Execute(context.Background(), map[string]interface{}{
		"command": "mkdir testdir",
	})

	if result.Success {
		t.Error("expected block for write command in supervised tier when called directly (must use ToolExecutor)")
	}
}

func TestShell_PromptAllowedWithApproval(t *testing.T) {
	// When the call arrives via the tool-execution pipeline, the eino
	// ToolWrapper marks the context with WithApprovalHandled after routing
	// through the ApprovalGate. In that case a supervised-tier write command
	// must execute (the user already approved it) rather than being
	// conservatively blocked by the raw-registry defence-in-depth check.
	tool := NewShell(t.TempDir(), security.TierSupervised, config.ToolsShellConfig{}, config.SandboxConfig{})
	ctx := security.WithApprovalHandled(context.Background())
	result := tool.Execute(ctx, map[string]interface{}{
		"command": "mkdir testdir",
	})

	if !result.Success {
		t.Errorf("expected success for approved write command in supervised tier, got error: %s", result.Error)
	}
}

func TestShell_MissingCommand(t *testing.T) {
	tool := NewShell(t.TempDir(), security.TierFull, config.ToolsShellConfig{}, config.SandboxConfig{})
	result := tool.Execute(context.Background(), map[string]interface{}{})

	if result.Success {
		t.Error("expected failure without command")
	}
}
