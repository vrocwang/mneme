package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ProtocolVersion is the current extension protocol version supported by the host.
const ProtocolVersion = 1

// ── Proto manifest ───────────────────────────────────────────────────────────

// ProtoManifest describes a extension binary's metadata. The core reads this
// during the handshake phase after launching the extension subprocess.
type ProtoManifest struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description,omitempty"`
	Tools        []string               `json:"tools"`                   // tool names provided by this extension
	AgentDefs    []string               `json:"agent_defs"`              // agent IDs provided (optional)
	ConfigSchema map[string]interface{} `json:"config_schema,omitempty"` // JSON Schema for extension settings
	ProtocolMin  int                    `json:"protocol_min"`            // minimum protocol version required
}

// ── Proto JSON-RPC protocol messages ─────────────────────────────────────────

// protoRequest is a JSON-RPC 2.0 request sent to a extension subprocess over stdin.
type protoRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// protoResponse is a JSON-RPC 2.0 response from a extension subprocess on stdout.
type protoResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *protoRPCErr    `json:"error,omitempty"`
}

type protoRPCErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Wire format for each method.

type protoListToolsResult struct {
	Tools []protoToolDef `json:"tools"`
}

type protoToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Permission  string                 `json:"permission,omitempty"`
	HasEffects  bool                   `json:"has_effects,omitempty"`
}

type protoCallToolParams struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type protoCallToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

type protoConfigureParams struct {
	Config map[string]interface{} `json:"config"`
}

type protoConfigureResult struct {
	Accepted bool   `json:"accepted"`
	Error    string `json:"error,omitempty"`
}

// StartProtoFromCommand parses a command string that may be a simple
// binary path or an interpreter invocation. When the path points to a
// script file (no execute bit), it reads the shebang line to find the
// interpreter. Falls back to extension-based detection.
func StartProtoFromCommand(ctx context.Context, command string, log *slog.Logger) (*ProtoProcess, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty extension command")
	}
	// Multi-word command: interpreter-style (e.g. "python3 script.py").
	if len(parts) > 1 {
		script := parts[len(parts)-1]
		interp := parts[:len(parts)-1]
		return StartProto(ctx, log, script, interp...)
	}
	// Single path.
	path := parts[0]
	if isNativeBinary(path) {
		return StartProto(ctx, log, path)
	}
	// Retry with .exe extension (platform-agnostic: Go on Windows produces .exe,
	// and cross-compiled binaries may be run on any OS).
	if filepath.Ext(path) == "" {
		exePath := path + ".exe"
		if isNativeBinary(exePath) {
			return StartProto(ctx, log, exePath)
		}
	}
	// Script file — resolve interpreter from shebang or extension.
	interp := resolveInterpreter(path)
	if interp == nil {
		return nil, fmt.Errorf("cannot determine interpreter for %q (no shebang, unknown extension)", path)
	}
	return StartProto(ctx, log, path, interp...)
}

// isNativeBinary returns true if path points to a compiled native binary
// (Go, C, Rust, etc.). Detects by magic number regardless of file extension,
// so it works on Windows systems where Go may not append .exe.
func isNativeBinary(path string) bool {
	// Check the path as-is first.
	if isNativeBinaryFile(path) {
		return true
	}
	// Try .exe extension (Windows convention, but not universal).
	if filepath.Ext(path) == "" {
		if isNativeBinaryFile(path + ".exe") {
			return true
		}
	}
	return false
}

// isNativeBinaryFile checks a single concrete path for a native binary.
func isNativeBinaryFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	// Known executable extensions.
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".exe" {
		return true
	}
	// Unix execute bit.
	if info.Mode()&0111 != 0 {
		return true
	}
	// No execute bit, no known extension — check magic number.
	// PE (Windows): MZ, ELF (Linux): \x7fELF, Mach-O (macOS): feedface/cafebabe/cfafed00
	return hasBinaryMagic(path)
}

// isScriptFile returns true if the file extension suggests a script.
func isScriptFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".py", ".js", ".mjs", ".ts", ".rb", ".sh", ".bash", ".lua", ".pl", ".php":
		return true
	}
	return false
}

// hasBinaryMagic reads the first 4 bytes of a file and checks for known
// native binary magic numbers: PE (MZ), ELF (\x7fELF), Mach-O variants.
func hasBinaryMagic(path string) bool {
	// Only check files without a script extension.
	if isScriptFile(path) {
		return false
	}
	// Require a minimum file size (skip tiny files).
	info, err := os.Stat(path)
	if err != nil || info.Size() < 2048 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false
	}
	// PE: MZ (0x4D 0x5A)
	if magic[0] == 0x4D && magic[1] == 0x5A {
		return true
	}
	// ELF: \x7f E L F
	if magic[0] == 0x7F && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F' {
		return true
	}
	// Mach-O 32-bit: CE FA ED FE or FE ED FA CE
	if magic[0] == 0xCE && magic[1] == 0xFA && magic[2] == 0xED && magic[3] == 0xFE {
		return true
	}
	if magic[0] == 0xFE && magic[1] == 0xED && magic[2] == 0xFA && magic[3] == 0xCE {
		return true
	}
	// Mach-O 64-bit: CF FA ED FE or FE ED FA CF
	if magic[0] == 0xCF && magic[1] == 0xFA && magic[2] == 0xED && magic[3] == 0xFE {
		return true
	}
	if magic[0] == 0xFE && magic[1] == 0xED && magic[2] == 0xFA && magic[3] == 0xCF {
		return true
	}
	return false
}

// resolveInterpreter reads the shebang line or falls back to extension mapping.
func resolveInterpreter(path string) []string {
	// Try shebang first.
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		var first [128]byte
		n, _ := f.Read(first[:])
		line := strings.TrimSpace(string(first[:n]))
		if strings.HasPrefix(line, "#!") {
			line = strings.TrimPrefix(line, "#!")
			line = strings.TrimSpace(line)
			// Strip /usr/bin/env prefix.
			line = strings.TrimPrefix(line, "/usr/bin/env ")
			line = strings.TrimPrefix(line, "/bin/env ")
			// Split shebang into interpreter + optional args.
			return strings.Fields(line)
		}
	}

	// Extension fallback.
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".py":
		return []string{"python3"}
	case ".js", ".mjs":
		return []string{"node"}
	case ".ts":
		return []string{"npx", "tsx"}
	case ".rb":
		return []string{"ruby"}
	case ".sh", ".bash":
		return []string{"bash"}
	case ".lua":
		return []string{"lua"}
	case ".pl":
		return []string{"perl"}
	case ".php":
		return []string{"php"}
	}
	return nil
}

// ── ProtoProcess ─────────────────────────────────────────────────────────────

// ProtoProcess represents a running extension subprocess connected via
// stdin/stdout JSON-RPC. It is the Go-side handle for the subprocess.
type ProtoProcess struct {
	Manifest ProtoManifest
	toolList []string // local copy of tool names

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	reqID  int64
	log    *slog.Logger
	cancel context.CancelFunc
	done   chan struct{}

	// Restart fields: stored so the extension can be re-launched after a crash.
	extensionPath string
	extensionArgs []string

	// Crashed is set when stderr monitoring detects a fatal error (panic,
	// fatal, SIGSEGV). Cleared on successful restart.
	crashed    bool
	crashedAt  time.Time
	stderrTail string // last ~1KB of stderr for diagnostics
}

// StartProto launches a extension and performs the handshake. extensionPath is
// the executable (or script). Additional args are passed before the extension
// path when an interpreter is used (e.g. StartProto(ctx, log, "script.py",
// "python3")). For native binaries, omit args.
func StartProto(ctx context.Context, log *slog.Logger, extensionPath string, args ...string) (*ProtoProcess, error) {
	if log == nil {
		log = slog.Default()
	}
	log = log.With("extension", extensionPath)

	// On Windows, binaries without a PATHEXT extension (.exe/.com/.bat/.cmd)
	// cannot be executed by Go's exec.Command. Rename to add .exe.
	if runtime.GOOS == "windows" && len(args) == 0 && filepath.Ext(extensionPath) == "" {
		exePath := extensionPath + ".exe"
		if err := os.Rename(extensionPath, exePath); err == nil {
			extensionPath = exePath
		}
	}
	procCtx, cancel := context.WithCancel(ctx)
	cmdArgs := append(args, extensionPath)
	cmd := exec.CommandContext(procCtx, cmdArgs[0], cmdArgs[1:]...)
	// Windows: prevent extensions from opening visible console windows.
	hideConsoleWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("extension stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("extension stdout pipe: %w", err)
	}
	// Stderr is captured for debugging but not parsed.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("extension stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("extension start: %w", err)
	}

	pp := &ProtoProcess{
		cmd:           cmd,
		stdin:         stdin,
		stdout:        bufio.NewReader(stdoutPipe),
		mu:            sync.Mutex{},
		reqID:         1,
		log:           log,
		cancel:        cancel,
		done:          make(chan struct{}),
		extensionPath: extensionPath,
		extensionArgs: args,
	}

	// Drain stderr in the background. Crash keywords (panic, fatal, SIGSEGV)
	// are detected and surfaced as log.Error for alerting. A ring buffer
	// preserves the last ~1KB of stderr for diagnostics.
	ring := make([]byte, 1024)
	var ringPos int
	go func() {
		defer close(pp.done)
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			// Append to ring buffer with line separator.
			entry := line + "\n"
			for i := 0; i < len(entry); i++ {
				ring[ringPos%len(ring)] = entry[i]
				ringPos++
			}

			// Build the current tail string from the ring buffer.
			tail := ringTail(ring, ringPos)
			pp.mu.Lock()
			pp.stderrTail = tail
			pp.mu.Unlock()

			lower := strings.ToLower(line)
			if strings.Contains(lower, "panic:") ||
				strings.Contains(lower, "fatal:") ||
				strings.Contains(lower, "sigsegv") ||
				strings.Contains(lower, "segmentation fault") ||
				strings.Contains(lower, "fatal error") {
				pp.mu.Lock()
				pp.crashed = true
				pp.crashedAt = time.Now()
				pp.mu.Unlock()
				log.Error("extension crash detected via stderr",
					"extension", extensionPath,
					"line", line,
					"stderr_tail", truncate(tail, 256))
			} else {
				log.Debug("extension stderr", "line", line)
			}
		}
		// Process exited — check if it was a crash (non-zero exit).
		if cmd.ProcessState != nil && !cmd.ProcessState.Success() {
			pp.mu.Lock()
			if !pp.crashed {
				pp.crashed = true
				pp.crashedAt = time.Now()
			}
			pp.mu.Unlock()
			log.Warn("extension process exited with error",
				"extension", extensionPath,
				"exit_code", cmd.ProcessState.ExitCode(),
				"stderr_tail", truncate(ringTail(ring, ringPos), 256))
		}
	}()

	// Handshake: extension must respond with its manifest.
	resp, err := pp.call(ctx, "extension.describe", nil)
	if err != nil {
		pp.Stop()
		return nil, fmt.Errorf("extension handshake (extension.describe): %w", err)
	}

	var manifest ProtoManifest
	if err := json.Unmarshal(resp.Result, &manifest); err != nil {
		pp.Stop()
		return nil, fmt.Errorf("extension manifest parse: %w", err)
	}
	if manifest.Name == "" {
		pp.Stop()
		return nil, fmt.Errorf("extension manifest missing name")
	}
	// Enforce ProtocolMin: reject extensions that require a newer protocol
	// version than the host supports.
	if manifest.ProtocolMin > ProtocolVersion {
		pp.Stop()
		return nil, fmt.Errorf("extension requires protocol v%d but host only supports v%d",
			manifest.ProtocolMin, ProtocolVersion)
	}
	pp.Manifest = manifest
	pp.log = log.With("extension_name", manifest.Name)
	pp.log.Info("extension connected", "version", manifest.Version, "tools", manifest.Tools, "protocol_min", manifest.ProtocolMin)

	return pp, nil
}

// call sends a JSON-RPC request to the extension and waits for the response.
// Holds the mutex for the entire request—response cycle to prevent concurrent
// calls from interleaving stdout reads and mismatching responses.
func (pp *ProtoProcess) call(ctx context.Context, method string, params interface{}) (*protoResponse, error) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	id := pp.reqID
	pp.reqID++

	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = b
	}

	req := protoRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if _, err := fmt.Fprintf(pp.stdin, "%s\n", reqBytes); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read response line — mutex is held so no other call() can interleave.
	line, err := pp.stdout.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp protoResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if resp.ID != id {
		return nil, fmt.Errorf("response id mismatch: expected %d, got %v", id, resp.ID)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("extension error [%d]: %s", resp.Error.Code, resp.Error.Message)
	}
	return &resp, nil
}

// ListTools asks the extension for its tool definitions.
func (pp *ProtoProcess) ListTools(ctx context.Context) ([]Tool, error) {
	resp, err := pp.call(ctx, "extension.list_tools", nil)
	if err != nil {
		return nil, err
	}
	var result protoListToolsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse tool list: %w", err)
	}

	tools := make([]Tool, 0, len(result.Tools))
	for _, td := range result.Tools {
		pt := &protoTool{
			proc: pp,
			def:  td,
		}
		tools = append(tools, pt)
		pp.toolList = append(pp.toolList, td.Name)
	}
	return tools, nil
}

// AgentDef mirrors the agent definition structure returned by
// extension.list_agents in extension binaries (e.g. skill-runtime).
type AgentDef struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tier          string   `json:"tier"`
	SystemPrompt  string   `json:"systemPrompt"`
	ToolAllowlist []string `json:"toolAllowlist"`
	ToolDenylist  []string `json:"toolDenylist,omitempty"`
	MaxIterations int      `json:"maxIterations"`
	Hidden        bool     `json:"hidden"`

	// ForkMode when true means this agent inherits the parent's full tool
	// registry without filtering. Used for agents that need broad access
	// to act on the parent's behalf.
	ForkMode bool `json:"forkMode,omitempty"`

	// Extended fields (migrated from agent.AgentDef).
	Model        string           `json:"model,omitempty"`
	Temperature  float64          `json:"temperature,omitempty"`
	TimeoutSecs  int              `json:"timeoutSecs,omitempty"`
	SandboxMode  string           `json:"sandboxMode,omitempty"`
	ExtraTools   []string         `json:"extraTools,omitempty"`
	Background   bool             `json:"background,omitempty"`
	SkillFilter  string           `json:"skillFilter,omitempty"`
	SubagentRefs []SubagentRef    `json:"subagents,omitempty"`
	ToolPolicy   *ProtoToolPolicy `json:"toolPolicy,omitempty"`
}

// SubagentRef represents an agent this definition can spawn.
type SubagentRef struct {
	AgentID      string `json:"agent_id"`
	SkillsFilter string `json:"skills_filter,omitempty"`
}

// ProtoToolPolicy defines per-tool access control for a extension agent.
type ProtoToolPolicy struct {
	RequireApprovalFor []string `json:"requireApprovalFor,omitempty"`
	DenyTools          []string `json:"denyTools,omitempty"`
	MaxToolRounds      int      `json:"maxToolRounds,omitempty"`
}

// IsToolDenied checks if a tool is explicitly denied.
func (p *ProtoToolPolicy) IsToolDenied(toolName string) bool {
	if p == nil {
		return false
	}
	for _, d := range p.DenyTools {
		if d == toolName {
			return true
		}
	}
	return false
}

// NeedsToolApproval checks if a tool requires user approval.
func (p *ProtoToolPolicy) NeedsToolApproval(toolName string) bool {
	if p == nil {
		return false
	}
	for _, a := range p.RequireApprovalFor {
		if a == toolName {
			return true
		}
	}
	return false
}

type protoListAgentsResult struct {
	Agents []AgentDef `json:"agents"`
}

// ListAgents asks the extension for its agent definitions. Extensions that
// don't implement extension.list_agents return an empty list gracefully.
func (pp *ProtoProcess) ListAgents(ctx context.Context) ([]AgentDef, error) {
	resp, err := pp.call(ctx, "extension.list_agents", nil)
	if err != nil {
		// Extension binaries that don't support extension.list_agents will
		// return an error; treat as empty agent list, not a fatal error.
		pp.log.Debug("extension.list_agents not supported", "extension", pp.Manifest.Name, "error", err)
		return nil, nil
	}
	var result protoListAgentsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		pp.log.Debug("extension.list_agents parse failed", "extension", pp.Manifest.Name, "error", err)
		return nil, nil
	}
	return result.Agents, nil
}

// Configure sends configuration to the extension. Extensions may accept or reject
// the config based on their ConfigSchema.
func (pp *ProtoProcess) Configure(ctx context.Context, config map[string]interface{}) error {
	params := protoConfigureParams{Config: config}
	resp, err := pp.call(ctx, "extension.configure", params)
	if err != nil {
		return fmt.Errorf("extension.configure: %w", err)
	}
	var result protoConfigureResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("parse configure result: %w", err)
	}
	if !result.Accepted {
		return fmt.Errorf("extension rejected config: %s", result.Error)
	}
	return nil
}

// Stop terminates the extension subprocess and reaps the child process to
// prevent zombie accumulation. Safe to call multiple times.
func (pp *ProtoProcess) Stop() error {
	pp.cancel()
	// Give the process a moment to exit gracefully.
	select {
	case <-pp.done:
		// Reap the child process to release OS resources.
		if pp.cmd != nil && pp.cmd.Process != nil {
			pp.cmd.Wait()
		}
		return nil
	case <-time.After(2 * time.Second):
		if pp.cmd.Process != nil {
			pp.cmd.Process.Kill()
			pp.cmd.Wait() // reap after kill
		}
	}
	return nil
}

// IsAlive returns true if the extension subprocess is still running.
func (pp *ProtoProcess) IsAlive() bool {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if pp.cmd == nil || pp.cmd.Process == nil {
		return false
	}
	if pp.crashed {
		return false
	}
	// Check if the process is still running.
	select {
	case <-pp.done:
		return false
	default:
		return true
	}
}

// IsCrashed returns true if stderr monitoring detected a fatal error.
func (pp *ProtoProcess) IsCrashed() bool {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return pp.crashed
}

// Ping sends a lightweight liveness probe (extension.describe) to verify the
// extension is truly responsive, not just alive at the process level.
// Used by the health monitor to detect hung extensions.
func (pp *ProtoProcess) Ping(ctx context.Context) error {
	_, err := pp.call(ctx, "extension.describe", nil)
	return err
}

// StderrTail returns the last ~1KB of stderr output for diagnostics.
func (pp *ProtoProcess) StderrTail() string {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return pp.stderrTail
}

// Restart stops the current extension process (if running) and launches a new
// one with the same path and arguments. Returns the new ProtoProcess handle
// (the receiver is updated in-place).
func (pp *ProtoProcess) Restart(ctx context.Context) error {
	pp.Stop()

	newProc, err := StartProto(ctx, pp.log, pp.extensionPath, pp.extensionArgs...)
	if err != nil {
		return fmt.Errorf("extension restart: %w", err)
	}

	// Copy state from the new process into the receiver.
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.cmd = newProc.cmd
	pp.stdin = newProc.stdin
	pp.stdout = newProc.stdout
	pp.Manifest = newProc.Manifest
	pp.toolList = newProc.toolList
	pp.reqID = 1
	pp.cancel = newProc.cancel
	pp.done = newProc.done
	pp.crashed = false
	pp.stderrTail = ""

	pp.log.Info("extension restarted", "extension", pp.extensionPath, "version", pp.Manifest.Version)
	return nil
}

// ringTail reconstructs the last used bytes of a ring buffer as a string,
// handling wrap-around. ringPos is the total number of bytes written.
func ringTail(ring []byte, ringPos int) string {
	n := len(ring)
	if ringPos < n {
		return string(ring[:ringPos])
	}
	start := ringPos % n
	// Data wraps: [start..n-1] then [0..start-1].
	var b strings.Builder
	b.Grow(n)
	b.Write(ring[start:])
	b.Write(ring[:start])
	return b.String()
}

// truncate returns s truncated to maxLen characters with "..." appended.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ── protoTool — Tool implementation that delegates to a extension subprocess ────

type protoTool struct {
	proc *ProtoProcess
	def  protoToolDef
}

func (pt *protoTool) Schema() Schema {
	return Schema{
		Name:        pt.def.Name,
		Description: pt.def.Description,
		Parameters:  pt.def.Parameters,
	}
}

func (pt *protoTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	params := protoCallToolParams{
		Name: pt.def.Name,
		Args: args,
	}
	resp, err := pt.proc.call(ctx, "extension.call_tool", params)
	if err != nil {
		return Result{Success: false, Error: fmt.Sprintf("extension call: %v", err)}
	}
	var cr protoCallToolResult
	if err := json.Unmarshal(resp.Result, &cr); err != nil {
		return Result{Success: false, Error: fmt.Sprintf("parse extension result: %v", err)}
	}
	return Result{Success: cr.Success, Output: cr.Output, Error: cr.Error}
}

// Proto tools default to execute permission and having external effects
// (fail-closed for security), since we can't introspect the extension binary.
func (pt *protoTool) PermissionLevel() PermissionLevel {
	return ParsePermissionLevel(pt.def.Permission)
}

func (pt *protoTool) PermissionLevelWithArgs(args map[string]interface{}) PermissionLevel {
	return pt.PermissionLevel()
}

func (pt *protoTool) SideEffects() bool {
	return pt.def.HasEffects
}

func (pt *protoTool) SideEffectsWithArgs(args map[string]interface{}) bool {
	return pt.SideEffects()
}

func (pt *protoTool) Category() ToolCategory {
	return CategorySkill
}

func (pt *protoTool) Descriptor() ToolDescriptor {
	return ToolDescriptor{
		Name:        pt.def.Name,
		Description: pt.def.Description,
		Source:      SourceExtension,
		Scope:       ScopeAll,
		Category:    CategorySkill,
		Permission:  pt.PermissionLevel(),
		Version:     pt.proc.Manifest.Version,
		Enabled:     true,
	}
}

// Compile-time check: protoTool implements extended interfaces.
var (
	_ PermissionedTool = (*protoTool)(nil)
	_ SideEffectTool   = (*protoTool)(nil)
	_ CategorizedTool  = (*protoTool)(nil)
	_ DescribedTool    = (*protoTool)(nil)
)
