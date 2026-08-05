package agent_tool_policy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/simon/mneme/internal/tools"
)

// ToolPolicyAction is the decision for a single tool.
type ToolPolicyAction string

const (
	ActionAllow           ToolPolicyAction = "allow"
	ActionRequireApproval ToolPolicyAction = "require_approval"
	ActionDeny            ToolPolicyAction = "deny"
	ActionHideFromPrompt  ToolPolicyAction = "hide_from_prompt"
)

// TaskRiskLevel classifies the overall risk of a task.
type TaskRiskLevel string

const (
	RiskLow      TaskRiskLevel = "low"
	RiskMedium   TaskRiskLevel = "medium"
	RiskHigh     TaskRiskLevel = "high"
	RiskCritical TaskRiskLevel = "critical"
)

// RiskLevelFromPermission derives the task risk level from the maximum
// allowed permission level.
func RiskLevelFromPermission(perm tools.PermissionLevel) TaskRiskLevel {
	switch perm {
	case tools.PermNone:
		return RiskLow
	case tools.PermReadOnly:
		return RiskLow
	case tools.PermWrite:
		return RiskMedium
	case tools.PermExecute:
		return RiskHigh
	case tools.PermDangerous:
		return RiskCritical
	default:
		return RiskHigh
	}
}

// TaskProfile describes the current task context for policy resolution.
type TaskProfile struct {
	AgentID           string                `json:"agent_id"`
	Channel           string                `json:"channel"`
	EntryPoint        string                `json:"entry_point"`
	RiskLevel         TaskRiskLevel         `json:"risk_level"`
	AllowedPermission tools.PermissionLevel `json:"allowed_permission"`
}

// ToolPolicyDecision records the policy outcome for a single tool.
type ToolPolicyDecision struct {
	ToolName           string                `json:"tool_name"`
	Action             ToolPolicyAction      `json:"action"`
	RequiredPermission tools.PermissionLevel `json:"required_permission"`
	AllowedPermission  tools.PermissionLevel `json:"allowed_permission"`
}

// ToolPolicySession is a pre-computed snapshot of all tool decisions for
// a single session. Built once via ToolPolicyEngine.BuildSession and
// consulted on every tool execution without re-evaluating the full matrix.
type ToolPolicySession struct {
	Profile      TaskProfile                   `json:"profile"`
	AllowedTools []string                      `json:"allowed_tools"`
	BlockedTools []string                      `json:"blocked_tools"`
	HiddenTools  []string                      `json:"hidden_tools"`
	Decisions    map[string]ToolPolicyDecision `json:"decisions"`
}

// VisibleToolsForPrompt returns tool names the agent should see in the
// system prompt (excludes hidden tools).
func (s *ToolPolicySession) VisibleToolsForPrompt() []string {
	var visible []string
	for _, t := range s.AllowedTools {
		hidden := false
		for _, h := range s.HiddenTools {
			if t == h {
				hidden = true
				break
			}
		}
		if !hidden {
			visible = append(visible, t)
		}
	}
	return visible
}

// RestrictedToolCount returns the count of tools that are blocked or hidden.
func (s *ToolPolicySession) RestrictedToolCount() int {
	return len(s.BlockedTools) + len(s.HiddenTools)
}

// ── Engine ─────────────────────────────────────────────────────────────

// ChannelPermissions maps channel identifiers to their allowed permission
// level strings. An empty map preserves legacy unrestricted behavior.
type ChannelPermissions map[string]string

// ToolPolicyEngine resolves tool policies for a session by combining
// the agent's policy profile, channel permissions, and global security
// policy into a single immutable ToolPolicySession.
type ToolPolicyEngine struct {
	registry           *Registry
	channelPermissions ChannelPermissions
}

// NewToolPolicyEngine creates a new policy engine.
func NewToolPolicyEngine(reg *Registry, channelPerms ChannelPermissions) *ToolPolicyEngine {
	return &ToolPolicyEngine{
		registry:           reg,
		channelPermissions: channelPerms,
	}
}

// BuildSession computes a ToolPolicySession for the given task profile
// and tool list. The result is an immutable snapshot for the session
// duration.
func (e *ToolPolicyEngine) BuildSession(profile TaskProfile, toolList []tools.ToolDescriptor) *ToolPolicySession {
	session := &ToolPolicySession{
		Profile:   profile,
		Decisions: make(map[string]ToolPolicyDecision),
	}

	// Resolve channel-based permission cap.
	permCap := e.resolveChannelPermission(profile.Channel, profile.AllowedPermission)

	// Get agent-specific policy.
	agentPolicy := e.registry.Get(profile.AgentID)

	for _, td := range toolList {
		decision := e.evaluateTool(td, agentPolicy, permCap)
		session.Decisions[td.Name] = decision

		switch decision.Action {
		case ActionAllow:
			session.AllowedTools = append(session.AllowedTools, td.Name)
		case ActionRequireApproval:
			session.AllowedTools = append(session.AllowedTools, td.Name)
		case ActionDeny:
			session.BlockedTools = append(session.BlockedTools, td.Name)
		case ActionHideFromPrompt:
			session.HiddenTools = append(session.HiddenTools, td.Name)
			session.AllowedTools = append(session.AllowedTools, td.Name) // executable but hidden
		}
	}

	sort.Strings(session.AllowedTools)
	sort.Strings(session.BlockedTools)
	sort.Strings(session.HiddenTools)

	return session
}

// IsToolAllowed checks a single tool against the session snapshot.
func (s *ToolPolicySession) IsToolAllowed(toolName string) bool {
	dec, ok := s.Decisions[toolName]
	if !ok {
		return false
	}
	return dec.Action == ActionAllow || dec.Action == ActionRequireApproval || dec.Action == ActionHideFromPrompt
}

// NeedsApproval checks if the tool requires user approval.
func (s *ToolPolicySession) NeedsApproval(toolName string) bool {
	dec, ok := s.Decisions[toolName]
	if !ok {
		return true // fail-closed: unknown tools require approval
	}
	return dec.Action == ActionRequireApproval
}

func (e *ToolPolicyEngine) evaluateTool(td tools.ToolDescriptor, agentPolicy *Policy, permCap tools.PermissionLevel) ToolPolicyDecision {
	decision := ToolPolicyDecision{
		ToolName:           td.Name,
		RequiredPermission: td.Permission,
		AllowedPermission:  permCap,
	}

	// If the tool's permission exceeds the resolved cap, deny.
	if td.Permission > permCap {
		decision.Action = ActionDeny
		return decision
	}

	// Check agent-specific policy.
	if agentPolicy != nil {
		if !agentPolicy.IsToolAllowed(td.Name, mapPermToPolicy(td.Permission)) {
			decision.Action = ActionDeny
			return decision
		}
		if agentPolicy.NeedsApproval(mapPermToPolicy(td.Permission)) {
			decision.Action = ActionRequireApproval
			return decision
		}
	}

	// Default: allow.
	decision.Action = ActionAllow
	return decision
}

func (e *ToolPolicyEngine) resolveChannelPermission(channel string, defaultPerm tools.PermissionLevel) tools.PermissionLevel {
	if e.channelPermissions == nil {
		return defaultPerm
	}
	permStr, ok := e.channelPermissions[channel]
	if !ok {
		// Unknown channels default to read_only.
		return tools.PermReadOnly
	}
	parsed := tools.ParsePermissionLevel(permStr)
	if parsed > defaultPerm {
		return defaultPerm // cap at the task's allowed permission
	}
	return parsed
}

// ── Prompt rendering ──────────────────────────────────────────────────

// RenderPolicyBoundary generates a "## Tool Policy Boundary" section for
// the system prompt, informing the agent of its current tool access.
func (s *ToolPolicySession) RenderPolicyBoundary(maxBytes int) string {
	var b strings.Builder
	b.WriteString("## Tool Policy Boundary\n\n")
	b.WriteString(fmt.Sprintf("- Agent: %s\n", s.Profile.AgentID))
	if s.Profile.Channel != "" {
		b.WriteString(fmt.Sprintf("- Channel: %s\n", s.Profile.Channel))
	}
	b.WriteString(fmt.Sprintf("- Entry Point: %s\n", s.Profile.EntryPoint))
	b.WriteString(fmt.Sprintf("- Allowed Permission: %s\n", s.Profile.AllowedPermission))
	b.WriteString(fmt.Sprintf("- Risk Level: %s\n", s.Profile.RiskLevel))
	b.WriteString(fmt.Sprintf("- Allowed Tools: %d\n", len(s.AllowedTools)))

	restricted := s.RestrictedToolCount()
	if restricted > 0 {
		b.WriteString(fmt.Sprintf("- Restricted Tools (blocked or hidden): %d\n", restricted))
	}

	result := b.String()
	if maxBytes > 0 && len(result) > maxBytes {
		// Truncate at UTF-8 character boundary.
		result = result[:maxBytes]
		if idx := strings.LastIndexAny(result, "\n"); idx > 0 {
			result = result[:idx]
		}
	}
	return result
}

func mapPermToPolicy(perm tools.PermissionLevel) PermissionLevel {
	switch perm {
	case tools.PermNone:
		return PermReadOnly
	case tools.PermReadOnly:
		return PermReadOnly
	case tools.PermWrite:
		return PermWrite
	case tools.PermExecute:
		return PermExecute
	case tools.PermDangerous:
		return PermDangerous
	default:
		return PermExecute
	}
}
