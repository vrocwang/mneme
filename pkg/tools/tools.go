// Package tools defines the shared Tool interface and types used by both the
// main Mneme binary and externally-built extensions. Implementation details
// (transport, sandboxing, policy enforcement) remain in internal/.
package tools

import "context"

// ── Permission ──────────────────────────────────────────────────────────────

// PermissionLevel indicates the risk tier of a tool.
type PermissionLevel int

const (
	PermNone      PermissionLevel = iota // no special permissions needed
	PermReadOnly                         // only reads data
	PermWrite                            // writes data
	PermExecute                          // runs code/commands
	PermDangerous                        // destructive or high-risk
)

func (p PermissionLevel) String() string {
	switch p {
	case PermNone:
		return "none"
	case PermReadOnly:
		return "read_only"
	case PermWrite:
		return "write"
	case PermExecute:
		return "execute"
	case PermDangerous:
		return "dangerous"
	default:
		return "unknown"
	}
}

// ParsePermissionLevel parses a permission string.
func ParsePermissionLevel(s string) PermissionLevel {
	switch s {
	case "none":
		return PermNone
	case "read_only":
		return PermReadOnly
	case "write":
		return PermWrite
	case "execute":
		return PermExecute
	case "dangerous":
		return PermDangerous
	default:
		return PermExecute
	}
}

// ── Tool categories ─────────────────────────────────────────────────────────

// ToolCategory distinguishes system tools from user-installed skills.
type ToolCategory string

const (
	CategorySystem      ToolCategory = "system"
	CategorySkill       ToolCategory = "skill"
	CategoryInteraction ToolCategory = "interaction"
)

// ToolSource identifies where a tool was loaded from.
type ToolSource string

const (
	SourceCore      ToolSource = "core"
	SourceExtension ToolSource = "extension"
	SourceMCP       ToolSource = "mcp"
	SourceCustom    ToolSource = "custom"
)

// Transport describes how a tool communicates with the agent harness.
type Transport string

const (
	TransportInProcess Transport = "in_process"
	TransportJsonRpc   Transport = "json_rpc"
	TransportMcpStdio  Transport = "mcp_stdio"
)

// ToolHealth indicates the runtime health of a registered tool.
type ToolHealth string

const (
	HealthAvailable ToolHealth = "available"
	HealthUnknown   ToolHealth = "unknown"
	HealthDegraded  ToolHealth = "degraded"
	HealthUnhealthy ToolHealth = "unhealthy"
)

// ToolScope restricts where a tool is available.
type ToolScope string

const (
	ScopeAll       ToolScope = "all"
	ScopeAgentOnly ToolScope = "agent_only"
	ScopeCliOnly   ToolScope = "cli_only"
)

// ── Core types ──────────────────────────────────────────────────────────────

// DefaultMaxIterations is the fallback bound on an agent's tool-calling loop
// when neither the agent definition nor the config specifies one. It is the
// single source of truth shared by config defaults, agent-TOML loading, and the
// eino agent constructor.
const DefaultMaxIterations = 10

// DefaultTimeoutSecs is the tool execution timeout in seconds used when neither
// configuration nor the environment sets one. It lives in this package — which
// imports only "context" — so internal/config can reference it without
// importing internal/tools (internal/tools imports internal/config, so the
// reverse edge would be a cycle). internal/tools aliases it, and the config
// default for agent.limits.default_tool_timeout references it directly.
const DefaultTimeoutSecs = 120

// Schema describes a tool's JSON Schema for LLM function calling.
type Schema struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Result is the outcome of a tool execution.
type Result struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// ToolFailurePrefix marks tool output that reports a tool-level failure. The
// agent runtime detects it to flag the result as an error for the UI while
// still feeding the text back to the model.
const ToolFailurePrefix = "Tool error: "

// ToolFailure is a failure reported by a tool itself — a missing file, a
// command refused by policy, a timeout — as opposed to an infrastructure or
// programming error. The agent runtime converts it into tool output so the
// model can correct course (retry, pick another tool) instead of the turn
// aborting; the loop breaker still counts it as a failure, so a model that
// repeats the same failing call is still cut off.
type ToolFailure struct{ Message string }

func (e *ToolFailure) Error() string { return e.Message }

// Tool is the interface all tools must implement.
type Tool interface {
	Schema() Schema
	Execute(ctx context.Context, args map[string]interface{}) Result
}

// DescribedTool allows tools to provide a ToolDescriptor for introspection.
type DescribedTool interface {
	Tool
	Descriptor() ToolDescriptor
}

// ToolDescriptor carries metadata about a registered tool.
type ToolDescriptor struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Source        ToolSource             `json:"source"`
	Transport     Transport              `json:"transport"`
	Scope         ToolScope              `json:"scope"`
	Category      ToolCategory           `json:"category,omitempty"`
	Permission    PermissionLevel        `json:"permission"`
	Version       string                 `json:"version,omitempty"`
	FilePath      string                 `json:"file_path,omitempty"`
	InputSchema   map[string]interface{} `json:"input_schema,omitempty"`
	OutputSchema  map[string]interface{} `json:"output_schema,omitempty"`
	AllowedAgents []string               `json:"allowed_agents,omitempty"`
	Tags          []string               `json:"tags,omitempty"`
	Health        ToolHealth             `json:"health"`
	Enabled       bool                   `json:"enabled"`
}
