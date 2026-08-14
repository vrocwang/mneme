package tools

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/security"
	"github.com/simon/mneme/internal/security/sandbox"
)

// defaultSafeEnvVars lists environment variables that are safe to pass to
// child processes. These are functional variables that cannot contain secrets.
// SSH_AUTH_SOCK / SSH_AGENT_PID are deliberately excluded — they are ambient
// authority (the user's SSH keys) and must not be forwarded to agent-run
// commands without explicit approval. This list is used as a fallback when
// config does not specify SafeEnvVars.
var defaultSafeEnvVars = []string{
	"PATH", "HOME", "USER", "LOGNAME",
	"LANG", "LC_ALL", "LC_CTYPE", "LC_MESSAGES",
	"TERM", "COLORTERM", "SHELL",
	"TMPDIR", "TMP", "TEMP",
	"XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR",
	"DISPLAY", "WAYLAND_DISPLAY",
	"DBUS_SESSION_BUS_ADDRESS",
	"PWD", "OLDPWD",
	// EDITOR, VISUAL, PAGER, BROWSER intentionally excluded —
	// they are classified as dangerous env prefixes by classify.go
	// because they can redirect program execution to arbitrary binaries.
	"GIT_TERMINAL_PROMPT",
	"NODE_ENV",
	"RUST_LOG", "RUST_BACKTRACE",
	"GOPATH", "GOROOT", "GOMODCACHE",
	"PYTHONUNBUFFERED",
	"JAVA_HOME",
	"CUDA_VISIBLE_DEVICES",
}

const defaultMaxShellOutputBytes = 1 << 20 // 1MB — matches Rust MAX_OUTPUT_BYTES

// Shell executes a shell command through the security gate.
type Shell struct {
	workspaceRoot   string
	tier            security.Tier
	sandbox         sandbox.Backend
	sandboxRequired bool
	maxOutputBytes  int
	safeEnvVars     []string
	timeoutSec      int
}

func NewShell(workspaceRoot string, tier security.Tier, toolsShellCfg config.ToolsShellConfig, sandboxCfg config.SandboxConfig) *Shell {
	s := &Shell{
		workspaceRoot:  workspaceRoot,
		tier:           tier,
		maxOutputBytes: toolsShellCfg.MaxOutputBytes,
		safeEnvVars:    toolsShellCfg.SafeEnvVars,
	}

	// Resolve sandbox backend from config.
	switch {
	case sandboxCfg.Mode == "disabled":
		s.sandbox = sandbox.NoopBackend()
	case sandboxCfg.BackendOverride != "":
		s.sandbox = sandbox.NewByName(sandboxCfg.BackendOverride)
		s.sandboxRequired = sandbox.IsNoop(s.sandbox)
	default:
		s.sandbox = sandbox.Detect()
		s.sandboxRequired = sandboxCfg.Mode == "sandboxed" && sandbox.IsNoop(s.sandbox)
	}

	// Apply defaults for zero values.
	if s.maxOutputBytes == 0 {
		s.maxOutputBytes = defaultMaxShellOutputBytes
	}
	s.timeoutSec = 120 // default: 2 minutes
	if len(s.safeEnvVars) == 0 {
		s.safeEnvVars = defaultSafeEnvVars
	}

	return s
}

func (t *Shell) Schema() Schema {
	return Schema{
		Name:        "shell",
		Description: "Execute a shell command",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Shell command to execute",
				},
			},
			"required": []string{"command"},
		},
	}
}

func (t *Shell) PermissionLevel() PermissionLevel { return PermExecute }
func (t *Shell) SideEffects() bool                { return true }

func (t *Shell) Execute(ctx context.Context, args map[string]interface{}) Result {
	command, _ := args["command"].(string)
	if command == "" {
		return Result{Error: "command is required"}
	}

	// Security classification — Block catastrophic commands at the tool level
	// regardless of whether called through the tool executor or raw registry.
	// Prompt decisions (e.g. write commands in supervised tier) require
	// interactive approval. Calls arriving via the tool-execution pipeline
	// are marked by the eino ToolWrapper with WithApprovalHandled after
	// routing through the ApprovalGate. When called directly through the raw
	// registry (no such signal), Prompt-class commands are conservatively
	// blocked.
	_, decision, err := security.CheckGatedCommand(command, t.tier, true, true)
	if err != nil {
		return Result{Error: err.Error()}
	}
	if decision == security.Prompt && !security.IsApprovalHandled(ctx) {
		return Result{Error: fmt.Sprintf("command %q requires approval in %s tier; route through the tool executor for interactive approval", security.NormalizeBase(command), t.tier)}
	}

	// In readonly tier, additionally check the curated command allowlist.
	// supervised and full tiers rely on risk-level checks only (block_high_risk,
	// require_medium_approval) - the allowlist is too restrictive for agents
	// that need to run arbitrary commands the user may not know in advance.
	if t.tier == security.TierReadOnly {
		if !security.IsCommandAllowed(command) {
			return Result{Error: fmt.Sprintf("command %q not in allowlist", security.NormalizeBase(command))}
		}
	}

	shellBin, shellFlag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shellBin, shellFlag = "cmd", "/c"
	}
	if t.sandboxRequired {
		return Result{Error: "sandbox is required by configuration but no sandbox backend is available on this platform"}
	}
	cmd, err := t.sandbox.WrapCommand(ctx, t.workspaceRoot, shellBin, shellFlag, command)
	if err != nil {
		return Result{Error: fmt.Sprintf("sandbox wrap failed: %v", err)}
	}

	// Run the shell in its own process group so a timeout can kill the whole
	// tree (children spawned by `sh -c` included) rather than only the direct
	// child process.
	setProcessGroup(cmd)

	// Sanitize environment: clear all inherited vars and only pass safe ones.
	// This prevents leakage of API keys and secrets to child processes.
	cmd.Env = nil
	for _, key := range t.safeEnvVars {
		if val, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+val)
		}
	}

	// Execute with timeout to prevent indefinite hangs (e.g. interactive commands).
	type cmdResult struct {
		output []byte
		err    error
	}
	ch := make(chan cmdResult, 1)
	go func() {
		o, e := cmd.CombinedOutput()
		ch <- cmdResult{o, e}
	}()
	var output []byte
	select {
	case r := <-ch:
		output, err = r.output, r.err
	case <-time.After(time.Duration(t.timeoutSec) * time.Second):
		killProcessTree(cmd)
		return Result{Error: fmt.Sprintf("command timed out after %d seconds", t.timeoutSec)}
	}
	outStr := string(output)
	truncated := len(outStr) > t.maxOutputBytes
	if truncated {
		outStr = safeTruncate(outStr, t.maxOutputBytes)
	}
	if err != nil {
		return Result{
			Output: outStr,
			Error:  fmt.Sprintf("command failed: %v", err),
		}
	}

	result := strings.TrimSpace(outStr)
	if truncated {
		result += fmt.Sprintf("\n...[output truncated at %d bytes]", t.maxOutputBytes)
	}
	return Result{
		Success: true,
		Output:  result,
	}
}

// safeTruncate truncates s to maxLen bytes at a valid UTF-8 boundary
// so multi-byte characters are not split mid-rune.
func safeTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	for end := maxLen; end > maxLen-4 && end > 0; end-- {
		if utf8.RuneStart(s[end]) {
			return s[:end]
		}
	}
	return s[:maxLen]
}
