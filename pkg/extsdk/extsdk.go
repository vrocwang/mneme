// Package extsdk is the shared SDK for Mneme extension binaries. It provides
// the JSON-RPC server loop, wire types, and handler registration so an
// extension only has to implement its tools — the protocol plumbing is
// provided here instead of being copy-pasted into every extension's main.go.
//
// An extension's main() is reduced to:
//
//	srv := extsdk.NewServer(extsdk.Manifest{Name: "x", Version: "0.1.0", ...})
//	srv.RegisterTool(extsdk.ToolDef{...}, handler)
//	if err := srv.Run(); err != nil { ... }
//
// The wire format is line-delimited JSON-RPC 2.0 over stdin/stdout and must
// stay compatible with the host-side protocol in internal/tools/extension_proto.go.
package extsdk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// ── Workspace helpers ────────────────────────────────────────────────────

// WorkspaceDir returns the Mneme workspace directory. The host process exports
// MNEME_HOME before spawning extensions, so every extension shares the user's
// workspace instead of guessing from its own executable path. Falls back to the
// executable's directory for standalone/manual runs where MNEME_HOME is unset.
func WorkspaceDir() string {
	if dir := os.Getenv("MNEME_HOME"); dir != "" {
		return dir
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return "."
}

// DataDir returns the shared data directory (<workspace>/data) and creates it
// if needed. Extensions that persist state must use this so they write to the
// same store as the core rather than a private directory beside their binary.
func DataDir() string {
	dir := filepath.Join(WorkspaceDir(), "data")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// DataFile returns a path inside DataDir.
func DataFile(name string) string {
	return filepath.Join(DataDir(), name)
}

// SkillsDir returns the shared skills directory (<workspace>/skills), the same
// location the core uses for installed skill definitions.
func SkillsDir() string {
	dir := filepath.Join(WorkspaceDir(), "skills")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// ScreenshotsDir returns the shared screenshots directory
// (<workspace>/screenshots), matching the core desktop capture location.
func ScreenshotsDir() string {
	dir := filepath.Join(WorkspaceDir(), "screenshots")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// TaskBoardsDir returns the shared per-thread task-board directory
// (<workspace>/agent_task_boards), where the todos extension persists boards.
func TaskBoardsDir() string {
	dir := filepath.Join(WorkspaceDir(), "agent_task_boards")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// WorkflowsDir returns the shared user workflows directory
// (<workspace>/workflows), the same location the core reads WORKFLOW.md from
// for prompt guidance.
func WorkflowsDir() string {
	dir := filepath.Join(WorkspaceDir(), "workflows")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// WorkflowLogsDir returns the shared workflow run-log directory
// (<workspace>/workflow-logs).
func WorkflowLogsDir() string {
	dir := filepath.Join(WorkspaceDir(), "workflow-logs")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// DefaultCallTimeout bounds each tool execution. The previous hand-written
// loops wrapped every call in context.WithTimeout (typically 30–60s, up to
// 5m for long-running tools). Since the JSON-RPC transport has no cancellation
// signal, this default prevents a hung handler from blocking the extension
// process forever. Tools needing longer can call SetCallTimeout.
const DefaultCallTimeout = 60 * time.Second

// ProtocolVersion is the extension protocol version implemented by this SDK.
// It must match internal/tools.ProtocolVersion on the host side.
const ProtocolVersion = 1

// ── Wire types (must match host-side JSON tags) ──────────────────────────

// Manifest describes an extension during the extension.describe handshake.
type Manifest struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description,omitempty"`
	Tools        []string               `json:"tools"`
	AgentDefs    []string               `json:"agent_defs"`
	ConfigSchema map[string]interface{} `json:"config_schema,omitempty"`
	ProtocolMin  int                    `json:"protocol_min"`
}

// ToolDef describes one tool for extension.list_tools.
type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Permission  string                 `json:"permission,omitempty"`
	HasEffects  bool                   `json:"has_effects,omitempty"`
	// ConcurrencySafe declares that this tool may run concurrently with other
	// tool calls from the same agent; the host maps it onto
	// tools.ConcurrencySafeTool. Leave false unless the handler is safe to
	// overlap — it only ever opts a tool in, and the wire field is omitted when
	// false, so extensions that do not set it are byte-for-byte unchanged.
	ConcurrencySafe bool `json:"concurrency_safe,omitempty"`
	// TimeoutSecs is how long this tool may legitimately run (e.g. 300 for a
	// workflow await). The host clamps it to 1s..1h and an explicit per-call
	// override still wins; zero (the default) means the host's global tool
	// timeout applies, so extensions that do not set it are unchanged.
	TimeoutSecs int `json:"timeout_secs,omitempty"`
}

// Result is the outcome of a tool execution.
type Result struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// AgentDef describes an agent for extension.list_agents.
type AgentDef struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	Tier          string        `json:"tier"`
	SystemPrompt  string        `json:"systemPrompt"`
	ToolAllowlist []string      `json:"toolAllowlist"`
	ToolDenylist  []string      `json:"toolDenylist,omitempty"`
	MaxIterations int           `json:"maxIterations"`
	Hidden        bool          `json:"hidden"`
	ForkMode      bool          `json:"forkMode,omitempty"`
	Model         string        `json:"model,omitempty"`
	Temperature   float64       `json:"temperature,omitempty"`
	TimeoutSecs   int           `json:"timeoutSecs,omitempty"`
	SandboxMode   string        `json:"sandboxMode,omitempty"`
	ExtraTools    []string      `json:"extraTools,omitempty"`
	Background    bool          `json:"background,omitempty"`
	SkillFilter   string        `json:"skillFilter,omitempty"`
	SubagentRefs  []SubagentRef `json:"subagents,omitempty"`
	ToolPolicy    *ToolPolicy   `json:"toolPolicy,omitempty"`
}

// SubagentRef is an agent this definition can spawn.
type SubagentRef struct {
	AgentID      string `json:"agent_id"`
	SkillsFilter string `json:"skills_filter,omitempty"`
}

// ToolPolicy defines per-tool access control for an extension agent.
type ToolPolicy struct {
	RequireApprovalFor []string `json:"requireApprovalFor,omitempty"`
	DenyTools          []string `json:"denyTools,omitempty"`
	MaxToolRounds      int      `json:"maxToolRounds,omitempty"`
}

// ToolHandler implements a single tool. The context is cancelled when the
// host cancels the call or the process is shutting down.
type ToolHandler func(ctx context.Context, args map[string]interface{}) Result

// ConfigHandler is invoked for extension.configure. Return nil to accept the
// configuration; return an error to reject it (sent to the host as accepted=false).
type ConfigHandler func(config map[string]interface{}) error

// ── JSON-RPC frame types (internal) ──────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newRPCError(code int, msg string) *rpcError {
	return &rpcError{Code: code, Message: msg}
}

// ── Server ───────────────────────────────────────────────────────────────

// Server runs the extension-side JSON-RPC protocol on stdin/stdout.
type Server struct {
	manifest Manifest
	log      *slog.Logger

	mu          sync.RWMutex
	tools       []ToolDef
	handlers    map[string]ToolHandler
	agents      []AgentDef
	config      ConfigHandler
	callTimeout time.Duration

	// concurrent enables one-goroutine-per-extension.call_tool dispatch. Off by
	// default so every existing extension keeps the original one-request-at-a-
	// time semantics.
	concurrent atomic.Bool
	// writeMu makes each response a single line on stdout even when calls are
	// dispatched concurrently.
	writeMu sync.Mutex
}

// NewServer creates a Server with the given manifest. tool names in the
// manifest are informational; actual tool discovery is driven by RegisterTool.
func NewServer(m Manifest) *Server {
	if m.ProtocolMin == 0 {
		m.ProtocolMin = ProtocolVersion
	}
	// Emit [] rather than null for empty lists, matching the host's
	// expectation that Tools/AgentDefs are arrays.
	if m.Tools == nil {
		m.Tools = []string{}
	}
	if m.AgentDefs == nil {
		m.AgentDefs = []string{}
	}
	return &Server{
		manifest:    m,
		log:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		handlers:    make(map[string]ToolHandler),
		callTimeout: DefaultCallTimeout,
	}
}

// SetCallTimeout overrides the per-tool execution timeout (default
// DefaultCallTimeout). Must be called before Run.
func (s *Server) SetCallTimeout(d time.Duration) {
	if d > 0 {
		s.callTimeout = d
	}
}

// SetLogger replaces the default stderr logger.
func (s *Server) SetLogger(log *slog.Logger) {
	if log != nil {
		s.log = log
	}
}

// RegisterTool registers a tool definition and its handler. Panics on a
// duplicate name (programmer error at startup).
func (s *Server) RegisterTool(def ToolDef, h ToolHandler) {
	if h == nil {
		panic(fmt.Sprintf("extsdk: tool %q has nil handler", def.Name))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.handlers[def.Name]; dup {
		panic(fmt.Sprintf("extsdk: duplicate tool %q", def.Name))
	}
	s.handlers[def.Name] = h
	s.tools = append(s.tools, def)
	s.manifest.Tools = append(s.manifest.Tools, def.Name)
}

// RegisterAgent registers an agent definition.
func (s *Server) RegisterAgent(def AgentDef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents = append(s.agents, def)
	s.manifest.AgentDefs = append(s.manifest.AgentDefs, def.ID)
}

// SetConfigHandler installs the extension.configure handler. The default
// accepts any configuration.
func (s *Server) SetConfigHandler(h ConfigHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = h
}

// SetConcurrentDispatch controls how extension.call_tool requests are served.
//
// With concurrent dispatch enabled, every extension.call_tool runs in its own
// goroutine while the cheap methods (describe/list_tools/list_agents/configure)
// keep answering inline. That is what lets an extension serve a second request —
// for example a cancel tool — while a long-running call is still outstanding;
// the sequential loop cannot even read the second request until the first
// returns. Responses are still written one line at a time under a mutex, and on
// stdin EOF Run waits (bounded by eofDrainGrace) for in-flight calls to finish.
//
// The default is off, which preserves the original one-request-at-a-time
// semantics for every existing extension. Only turn it on when the registered
// handlers are safe to overlap. Must be called before Run.
func (s *Server) SetConcurrentDispatch(on bool) {
	s.concurrent.Store(on)
}

// eofDrainGrace bounds how long Run waits for in-flight extension.call_tool
// handlers after stdin reaches EOF. The host closes stdin on shutdown; a
// multi-minute call should not stall the process, but an almost-finished
// response should not be dropped either.
const eofDrainGrace = 10 * time.Second

// Run blocks, reading JSON-RPC requests from stdin and writing responses to
// stdout until stdin reaches EOF. It returns nil on clean shutdown (EOF).
func (s *Server) Run() error {
	return s.run(os.Stdin, os.Stdout)
}

// run is Run with injectable streams so tests can drive the protocol loop over
// pipes instead of the process's own stdin/stdout.
func (s *Server) run(in io.Reader, out io.Writer) error {
	s.log.Info("extension starting", "name", s.manifest.Name, "version", s.manifest.Version)
	reader := bufio.NewReader(in)
	concurrent := s.concurrent.Load()

	// wg tracks concurrently dispatched call_tool goroutines so EOF can wait
	// for them. It is empty (and unused) in the sequential default mode.
	var wg sync.WaitGroup

	for {
		line, err := reader.ReadBytes('\n')
		if err == nil && len(bytes.TrimSpace(line)) > 0 {
			var req rpcRequest
			if uerr := json.Unmarshal(line, &req); uerr != nil {
				s.log.Error("unmarshal request", "error", uerr)
			} else if concurrent && req.Method == "extension.call_tool" {
				wg.Add(1)
				go func(req rpcRequest) {
					defer wg.Done()
					s.writeResponse(out, s.handle(&req))
				}(req)
			} else {
				s.writeResponse(out, s.handle(&req))
			}
		}
		if err != nil {
			if err == io.EOF {
				s.log.Info("stdin closed, exiting")
				s.waitInFlight(&wg)
				return nil
			}
			s.waitInFlight(&wg)
			return fmt.Errorf("read request: %w", err)
		}
	}
}

// writeResponse frames one response as a single line. The write mutex keeps
// concurrently dispatched calls from interleaving their bytes on stdout.
func (s *Server) writeResponse(out io.Writer, resp *rpcResponse) {
	respBytes, err := json.Marshal(resp)
	if err != nil {
		s.log.Error("marshal response", "error", err)
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	fmt.Fprintf(out, "%s\n", respBytes)
}

// waitInFlight waits for concurrently dispatched call_tool goroutines, bounded
// by eofDrainGrace.
func (s *Server) waitInFlight(wg *sync.WaitGroup) {
	if !s.concurrent.Load() {
		return
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(eofDrainGrace):
		s.log.Warn("stdin closed with extension.call_tool calls still in flight", "grace", eofDrainGrace)
	}
}

func (s *Server) handle(req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "extension.describe", "manifest": // "manifest" is a legacy alias some extensions use
		result, _ := json.Marshal(s.manifest)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}

	case "extension.list_tools":
		s.mu.RLock()
		tools := s.tools
		s.mu.RUnlock()
		if tools == nil {
			tools = []ToolDef{} // emit [] rather than null
		}
		result, _ := json.Marshal(struct {
			Tools []ToolDef `json:"tools"`
		}{Tools: tools})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}

	case "extension.list_agents":
		s.mu.RLock()
		agents := s.agents
		s.mu.RUnlock()
		if agents == nil {
			agents = []AgentDef{} // emit [] rather than null
		}
		result, _ := json.Marshal(struct {
			Agents []AgentDef `json:"agents"`
		}{Agents: agents})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}

	case "extension.configure":
		var params struct {
			Config map[string]interface{} `json:"config"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.rpcResult(req, configureResult{Accepted: false, Error: "invalid config params"})
		}
		s.mu.RLock()
		cfgHandler := s.config
		s.mu.RUnlock()
		if cfgHandler == nil {
			return s.rpcResult(req, configureResult{Accepted: true})
		}
		if err := cfgHandler(params.Config); err != nil {
			return s.rpcResult(req, configureResult{Accepted: false, Error: err.Error()})
		}
		return s.rpcResult(req, configureResult{Accepted: true})

	case "extension.call_tool":
		var params struct {
			Name string                 `json:"name"`
			Args map[string]interface{} `json:"args"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.rpcResult(req, Result{Error: "invalid call_tool params"})
		}
		s.mu.RLock()
		h, ok := s.handlers[params.Name]
		s.mu.RUnlock()
		if !ok {
			return s.rpcResult(req, Result{Error: fmt.Sprintf("unknown tool: %s", params.Name)})
		}
		ctx, cancel := context.WithTimeout(context.Background(), s.callTimeout)
		defer cancel()
		res := h(ctx, params.Args)
		return s.rpcResult(req, res)

	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: newRPCError(-32601, fmt.Sprintf("unknown: %s", req.Method))}
	}
}

type configureResult struct {
	Accepted bool   `json:"accepted"`
	Error    string `json:"error,omitempty"`
}

// rpcResult wraps a result value into a JSON-RPC response, serialising it as
// the Result field.
func (s *Server) rpcResult(req *rpcRequest, v interface{}) *rpcResponse {
	b, err := json.Marshal(v)
	if err != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: newRPCError(-32603, err.Error())}
	}
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: b}
}

// ── Argument helpers (shared by extensions to avoid per-extension helpers) ──

// GetStr returns the string value at key, or fallback when absent/empty.
func GetStr(args map[string]interface{}, key, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

// GetInt returns the int value at key, or fallback when absent. Accepts
// float64 (JSON numbers), int, and int64.
func GetInt(args map[string]interface{}, key string, fallback int) int {
	v, ok := args[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return fallback
}

// GetBool returns the bool value at key, or fallback when absent.
func GetBool(args map[string]interface{}, key string, fallback bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return fallback
}
