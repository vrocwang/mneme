// Package agent_tool_policy defines per-agent tool policy types for
// fine-grained control over which tools each agent can access.
package agent_tool_policy

import (
	"sort"
	"strings"
)

// Policy defines tool access rules for an agent.
type Policy struct {
	// Allowlist: if non-empty, ONLY these tools are allowed.
	Allowlist []string `json:"allowlist,omitempty"`

	// Denylist: tools explicitly blocked (applied after allowlist).
	Denylist []string `json:"denylist,omitempty"`

	// PermissionCap is the maximum permission level this agent can use.
	// Even if a tool is on the allowlist, it won't execute if its level exceeds this cap.
	PermissionCap PermissionLevel `json:"permission_cap"`

	// RequireApprovalFor lists permission levels that always need user approval,
	// even in full-autonomy tier.
	RequireApprovalFor []PermissionLevel `json:"require_approval_for,omitempty"`

	// MaxConcurrent is the maximum number of concurrent tool calls this agent can make.
	// 0 means no limit.
	MaxConcurrent int `json:"max_concurrent"`

	// MaxRoundsPerTurn limits how many tool rounds the agent can use in a single turn.
	// 0 means use the system default.
	MaxRoundsPerTurn int `json:"max_rounds_per_turn"`
}

// PermissionLevel mirrors the tool system's permission tier.
type PermissionLevel int

const (
	PermReadOnly  PermissionLevel = 1
	PermWrite     PermissionLevel = 2
	PermExecute   PermissionLevel = 3
	PermDangerous PermissionLevel = 4
)

func (p PermissionLevel) String() string {
	switch p {
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

// DefaultPolicy returns a safe default policy (supervised mode friendly).
func DefaultPolicy() *Policy {
	return &Policy{
		PermissionCap:      PermExecute,
		RequireApprovalFor: []PermissionLevel{PermDangerous},
	}
}

// UnrestrictedPolicy returns a policy with no restrictions (full access).
func UnrestrictedPolicy() *Policy {
	return &Policy{
		PermissionCap: PermDangerous,
	}
}

// ReadOnlyPolicy returns a policy that only allows read-level tools.
func ReadOnlyPolicy() *Policy {
	return &Policy{
		PermissionCap: PermReadOnly,
	}
}

// IsToolAllowed checks whether a tool with the given name and permission level
// is permitted under this policy.
func (p *Policy) IsToolAllowed(name string, level PermissionLevel) bool {
	if p == nil {
		return true
	}

	// Permission cap check
	if level > p.PermissionCap {
		return false
	}

	// Denylist check (takes priority over allowlist for explicit blocks)
	for _, blocked := range p.Denylist {
		if matchToolName(blocked, name) {
			return false
		}
	}

	// Allowlist check
	if len(p.Allowlist) > 0 {
		for _, allowed := range p.Allowlist {
			if matchToolName(allowed, name) {
				return true
			}
		}
		return false
	}

	return true
}

// NeedsApproval checks whether this tool requires user approval under the policy.
func (p *Policy) NeedsApproval(level PermissionLevel) bool {
	if p == nil {
		return false
	}
	for _, l := range p.RequireApprovalFor {
		if l == level {
			return true
		}
	}
	return false
}

// FilterTools returns the subset of tool names that are allowed under this policy.
// toolLevels maps tool names to their permission levels.
func (p *Policy) FilterTools(toolLevels map[string]PermissionLevel) []string {
	if p == nil {
		all := make([]string, 0, len(toolLevels))
		for name := range toolLevels {
			all = append(all, name)
		}
		sort.Strings(all)
		return all
	}

	var allowed []string
	for name, level := range toolLevels {
		if p.IsToolAllowed(name, level) {
			allowed = append(allowed, name)
		}
	}
	sort.Strings(allowed)
	return allowed
}

// Profile associates a named policy preset with an agent.
type Profile struct {
	AgentID string `json:"agent_id"`
	Policy  Policy `json:"policy"`
}

// Registry holds per-agent policy profiles.
type Registry struct {
	profiles map[string]*Profile // keyed by agent ID
}

// NewRegistry creates a policy registry.
func NewRegistry() *Registry {
	return &Registry{profiles: make(map[string]*Profile)}
}

// Set assigns a policy to an agent.
func (r *Registry) Set(agentID string, p *Policy) {
	r.profiles[agentID] = &Profile{AgentID: agentID, Policy: *p}
}

// Get returns the policy for an agent, or nil if not set.
func (r *Registry) Get(agentID string) *Policy {
	if p, ok := r.profiles[agentID]; ok {
		cp := p.Policy
		return &cp
	}
	return nil
}

// Remove deletes the policy for an agent.
func (r *Registry) Remove(agentID string) {
	delete(r.profiles, agentID)
}

// List returns all configured profiles.
func (r *Registry) List() []Profile {
	result := make([]Profile, 0, len(r.profiles))
	for _, p := range r.profiles {
		result = append(result, *p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].AgentID < result[j].AgentID
	})
	return result
}

// ── Helpers ─────────────────────────────────────────────────────

// matchToolName supports exact match and wildcard suffix ("*").
func matchToolName(pattern, name string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(name, prefix)
	}
	return pattern == name
}

// ParsePermissionLevel converts a string to a PermissionLevel.
func ParsePermissionLevel(s string) PermissionLevel {
	switch strings.ToLower(s) {
	case "read_only", "readonly":
		return PermReadOnly
	case "write":
		return PermWrite
	case "execute":
		return PermExecute
	case "dangerous":
		return PermDangerous
	default:
		return PermReadOnly
	}
}
