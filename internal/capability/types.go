// Package capability provides a unified registry for all tools and agents,
// organized by source (CapabilitySet) rather than by kind.
package capability

import (
	"encoding/json"
)

// SourceKind identifies the origin of a capability set.
type SourceKind string

const (
	KindBuiltin    SourceKind = "builtin"
	KindExtension  SourceKind = "extension"
	KindMCPServer  SourceKind = "mcp_server"
	KindSkill      SourceKind = "skill"
	KindUserAgent  SourceKind = "user_agent"
	KindAgentPack  SourceKind = "agent_pack"
)

// SetHealth reports the connection health of a capability set.
type SetHealth string

const (
	HealthOK       SetHealth = "ok"
	HealthDegraded SetHealth = "degraded"
	HealthDown     SetHealth = "down"
	HealthUnknown  SetHealth = "unknown"
)

// ToolDescriptor is a lightweight tool metadata record.
type ToolDescriptor struct {
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	Permission        string          `json:"permission"`
	HasSideEffects    bool            `json:"has_side_effects"`
	IsConcurrencySafe bool            `json:"is_concurrency_safe"`
	InputSchema       json.RawMessage `json:"input_schema"`
}

// AgentDescriptor is a lightweight agent metadata record.
type AgentDescriptor struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tier          string   `json:"tier"`
	ToolAllowlist []string `json:"tool_allowlist,omitempty"`
	ToolDenylist  []string `json:"tool_denylist,omitempty"`
	MaxIterations int      `json:"max_iterations"`
	Hidden        bool     `json:"hidden"`
	Model         string   `json:"model,omitempty"`
	Temperature   float64  `json:"temperature,omitempty"`
	TimeoutSecs   int      `json:"timeout_secs,omitempty"`
	SandboxMode   string   `json:"sandbox_mode,omitempty"`
	Background    bool     `json:"background,omitempty"`
}

// CapabilitySet groups tools and agents from a single source.
type CapabilitySet struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Kind         SourceKind        `json:"kind"`
	Description  string            `json:"description,omitempty"`
	Tools        []ToolDescriptor  `json:"tools"`
	Agents       []AgentDescriptor `json:"agents"`
	Health       SetHealth         `json:"health"`
	Enabled      bool              `json:"enabled"`
	ConnectedAt  string            `json:"connected_at,omitempty"`
	ToolCount    int               `json:"tool_count"`
	AgentCount   int               `json:"agent_count"`
	ChannelCount int               `json:"channel_count,omitempty"`
	Config       json.RawMessage   `json:"config,omitempty"`
}

// ServerEntry is the config for an MCP server, stored in CapabilitySet.Config.
type ServerEntry struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Command   string   `json:"command,omitempty"`
	Args      []string `json:"args,omitempty"`
	URL       string   `json:"url,omitempty"`
	Enabled   bool     `json:"enabled"`

	// Per-server tool filtering (matches Rust McpServerDefinition.allowed_tools/disallowed_tools).
	// When AllowedTools is non-empty, only listed tools are registered.
	// DisallowedTools are always excluded, even if AllowedTools is empty.
	AllowedTools    []string `json:"allowed_tools,omitempty"`
	DisallowedTools []string `json:"disallowed_tools,omitempty"`
}

// SkillManifest describes a skill from a SKILL.md file's YAML frontmatter.
// Mirrors the agentskills.io / SKILL.md specification used by HermesHub, skills.sh, etc.
// Stored as the Config of a KindSkill CapabilitySet.
type SkillManifest struct {
	Name        string   `yaml:"name" json:"name"`
	Version     string   `yaml:"version" json:"version,omitempty"`
	Description string   `yaml:"description" json:"description,omitempty"`
	Author      string   `yaml:"author" json:"author,omitempty"`
	License     string   `yaml:"license" json:"license,omitempty"`
	Homepage    string   `yaml:"homepage" json:"homepage,omitempty"`
	Tags        []string `yaml:"tags" json:"tags,omitempty"`
	Tools       []string `yaml:"tools" json:"tools,omitempty"`
}

// SkillToolDef is a tool definition within a skill manifest (manifest.json variant).
type SkillToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Command     string          `json:"command"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// SkillAgentDef is an agent definition within a skill manifest (manifest.json variant).
type SkillAgentDef struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tier          string   `json:"tier"`
	SystemPrompt  string   `json:"system_prompt,omitempty"`
	ToolAllowlist []string `json:"tool_allowlist,omitempty"`
}
