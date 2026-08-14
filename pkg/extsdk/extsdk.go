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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

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
}

// Result is the outcome of a tool execution.
type Result struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// AgentDef describes an agent for extension.list_agents.
type AgentDef struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	Tier          string       `json:"tier"`
	SystemPrompt  string       `json:"systemPrompt"`
	ToolAllowlist []string     `json:"toolAllowlist"`
	ToolDenylist  []string     `json:"toolDenylist,omitempty"`
	MaxIterations int          `json:"maxIterations"`
	Hidden        bool         `json:"hidden"`
	ForkMode      bool         `json:"forkMode,omitempty"`
	Model         string       `json:"model,omitempty"`
	Temperature   float64      `json:"temperature,omitempty"`
	TimeoutSecs   int          `json:"timeoutSecs,omitempty"`
	SandboxMode   string       `json:"sandboxMode,omitempty"`
	ExtraTools    []string     `json:"extraTools,omitempty"`
	Background    bool         `json:"background,omitempty"`
	SkillFilter   string       `json:"skillFilter,omitempty"`
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

	mu      sync.RWMutex
	tools   []ToolDef
	handlers map[string]ToolHandler
	agents  []AgentDef
	config  ConfigHandler
}

// NewServer creates a Server with the given manifest. tool names in the
// manifest are informational; actual tool discovery is driven by RegisterTool.
func NewServer(m Manifest) *Server {
	if m.ProtocolMin == 0 {
		m.ProtocolMin = ProtocolVersion
	}
	return &Server{
		manifest: m,
		log:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		handlers: make(map[string]ToolHandler),
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

// Run blocks, reading JSON-RPC requests from stdin and writing responses to
// stdout until stdin reaches EOF. It returns nil on clean shutdown (EOF).
func (s *Server) Run() error {
	s.log.Info("extension starting", "name", s.manifest.Name, "version", s.manifest.Version)
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				s.log.Info("stdin closed, exiting")
				return nil
			}
			return fmt.Errorf("read request: %w", err)
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.log.Error("unmarshal request", "error", err)
			continue
		}
		resp := s.handle(&req)
		respBytes, err := json.Marshal(resp)
		if err != nil {
			s.log.Error("marshal response", "error", err)
			continue
		}
		fmt.Fprintf(os.Stdout, "%s\n", respBytes)
	}
}

func (s *Server) handle(req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "extension.describe":
		result, _ := json.Marshal(s.manifest)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}

	case "extension.list_tools":
		s.mu.RLock()
		tools := append([]ToolDef(nil), s.tools...)
		s.mu.RUnlock()
		result, _ := json.Marshal(struct {
			Tools []ToolDef `json:"tools"`
		}{Tools: tools})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}

	case "extension.list_agents":
		s.mu.RLock()
		agents := append([]AgentDef(nil), s.agents...)
		s.mu.RUnlock()
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
		ctx := context.Background()
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
