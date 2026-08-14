// Package eino provides an adapter layer that maps Mneme config to
// cloudwego/eino chat model instances and agent definitions.
package eino

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	einomw "github.com/simon/mneme/internal/eino/middleware"
	"github.com/simon/mneme/internal/prompts"
)

// AgentDef contains the definition needed to dynamically create an agent.
type AgentDef struct {
	ID            string
	Name          string
	Description   string
	ToolAllowlist []string
	Hidden        bool
	SystemPrompt  string // explicit prompt (agent pack prompt.md / toml system_prompt); takes precedence over embedded PromptName
}

// AgentRegistry provides agent definitions for dynamic agent creation.
// Implementations (e.g. capability.CapabilityRegistry via an adapter)
// supply the set of agents that NewAgentSet should create.
type AgentRegistry interface {
	AgentDefs() []AgentDef
}

// AgentSet holds all agents. The general agent is intended to be
// the top-level orchestrator; it receives all sub-agents as AgentTools.
type AgentSet struct {
	General      adk.Agent
	Orchestrator adk.Agent // backward compat
	Researcher   adk.Agent // backward compat
	// SubAgents holds all dynamically-created sub-agents.
	SubAgents []adk.Agent
}

// AgentSetConfig supplies the shared dependencies needed to create all
// 12 agents. It is intentionally minimal — additional per-agent
// configuration (e.g. temperature, custom tools) can be added as fields
// later without breaking the constructor signature.
type AgentSetConfig struct {
	// Workspace is the root directory for agent data.
	Workspace string

	// PromptMgr resolves system prompt templates by name.
	PromptMgr *prompts.Manager

	// ChatModel is the LLM backing every agent in the set.
	// It must satisfy model.ToolCallingChatModel so that the general
	// agent can bind sub-agent tools via WithTools.
	ChatModel model.ToolCallingChatModel

	// AllTools are the core (non-agent) tools available to the general agent.
	AllTools []tool.BaseTool

	// SecurityMW, when set, wraps every tool with approval gating and
	// credential scrubbing via ToolWrapper.
	SecurityMW *einomw.SecurityMiddleware

	// BreakerMW, when set, wraps every tool with failure/loop detection
	// via ToolWrapper.
	BreakerMW *einomw.CircuitBreakerMiddleware

	// FailoverModels, when non-empty, provides alternate chat models that
	// the agent can fall back to when the primary ChatModel fails.
	FailoverModels []model.ToolCallingChatModel

	// Handlers are lifecycle middleware applied to every agent.
	Handlers []adk.ChatModelAgentMiddleware

	// MessageModifier transforms the message list before each turn. It is
	// applied by the Runner before calling adk.Runner.Run().
	MessageModifier func(context.Context, []*schema.Message) []*schema.Message
}

// agentCfg holds the configuration for creating a single ChatModelAgent.
type agentCfg struct {
	Name            string
	Description     string
	PromptName      prompts.Name
	SystemPrompt    string // explicit prompt from agent def (agent pack prompt.md / toml system_prompt); takes precedence over PromptName
	Tools           []tool.BaseTool
	MessageModifier func(context.Context, []*schema.Message) []*schema.Message
}

// NewAgentSet creates all agents in three phases:
//  1. Create sub-agents from the registry (no AgentAsTools yet).
//  2. Wrap each sub-agent as an AgentTool via adk.NewAgentTool.
//  3. Create the general agent with sub-agent tools + core tools mounted.
//
// The ordering is critical: sub-agents must exist before they can be
// wrapped as tools. Sub-agents are created without tools of their own to
// keep the dependency graph acyclic.
//
// When registry is nil, no sub-agents are created (General only).
// When registry is non-nil, sub-agents are dynamically created from
// the registry's agent definitions.
func NewAgentSet(ctx context.Context, cfg *AgentSetConfig, registry AgentRegistry) (*AgentSet, error) {
	if cfg == nil {
		return nil, fmt.Errorf("eino: AgentSetConfig is nil")
	}
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("eino: ChatModel is nil")
	}

	agents := &AgentSet{}
	var err error

	// Wrap core tools with security/breaker. These wrappers are shared
	// between general and sub-agents - each sub-agent gets a role-filtered
	// subset (see filterToolsByAllowlist).
	wrappedTools := einomw.WrapAllTools(cfg.AllTools, cfg.SecurityMW, cfg.BreakerMW)

	// generalDef captures the registry's "general" agent definition (if any),
	// so the general agent is created from the registry like every other agent
	// rather than hardcoded. It defaults to a built-in fallback below.
	generalDef := AgentDef{
		ID:            "general",
		Name:          "General",
		Description:   "General-purpose orchestrator agent that coordinates specialists and handles conversations.",
		ToolAllowlist: []string{"*"},
	}

	// Phase 1 - create sub-agents dynamically from the registry.
	if registry != nil {
		for _, def := range registry.AgentDefs() {
			if def.ID == "general" {
				generalDef = def
				continue // General is created in Phase 3
			}
			if def.Hidden {
				continue // skip hidden agents
			}
			if def.Name == "" {
				def.Name = def.ID
			}
			agentTools := filterToolsByAllowlist(wrappedTools, def.ToolAllowlist)
			a, err := newAgent(ctx, agentCfg{
				Name:         def.Name,
				Description:  def.Description,
				PromptName:   prompts.NameFromAgentID(def.ID),
				SystemPrompt: def.SystemPrompt,
				Tools:        agentTools,
			}, cfg)
			if err != nil {
				return nil, fmt.Errorf("eino: create agent %q: %w", def.ID, err)
			}
			agents.SubAgents = append(agents.SubAgents, a)
		}
	}

	// Phase 2 - wrap sub-agents as tools. Sub-agent AgentTools also go
	// through the same security + breaker middleware as core tools so
	// the General agent's delegation is subject to approval gating.
	subAgentTools, err := createSubAgentTools(ctx, agents.SubAgents)
	if err != nil {
		return nil, fmt.Errorf("eino: wrap sub-agent tools: %w", err)
	}
	wrappedSubAgentTools := einomw.WrapAllTools(subAgentTools, cfg.SecurityMW, cfg.BreakerMW)

	// Phase 3 — create general agent with core tools + sub-agent tools. The
	// definition comes from the registry (so workspace/agents/general.toml can
	// override it); the tool set is always the full core + sub-agent set.
	generalTools := make([]tool.BaseTool, 0, len(wrappedTools)+len(wrappedSubAgentTools))
	generalTools = append(generalTools, wrappedTools...)
	generalTools = append(generalTools, wrappedSubAgentTools...)

	if generalDef.Name == "" {
		generalDef.Name = generalDef.ID
	}
	agents.General, err = newAgent(ctx, agentCfg{
		Name:            generalDef.Name,
		Description:     generalDef.Description,
		PromptName:      prompts.NameFromAgentID(generalDef.ID),
		SystemPrompt:    generalDef.SystemPrompt,
		Tools:           generalTools,
		MessageModifier: cfg.MessageModifier,
	}, cfg)
	if err != nil {
		return nil, fmt.Errorf("eino: create general agent: %w", err)
	}

	// Backward compat: set Orchestrator and Researcher to the first
	// sub-agents if they exist, so external code referencing these
	// fields still works.
	if len(agents.SubAgents) > 0 {
		agents.Orchestrator = agents.SubAgents[0]
	}
	if len(agents.SubAgents) > 1 {
		agents.Researcher = agents.SubAgents[1]
	}

	return agents, nil
}

// createSubAgentTools wraps every sub-agent as an AgentTool via adk.NewAgentTool.
func createSubAgentTools(ctx context.Context, subs []adk.Agent) ([]tool.BaseTool, error) {
	out := make([]tool.BaseTool, 0, len(subs))
	for _, a := range subs {
		t := adk.NewAgentTool(ctx, a)
		out = append(out, t)
	}
	return out, nil
}

// newAgent creates a single ChatModelAgent from the given configuration.
func newAgent(ctx context.Context, cfg agentCfg, setCfg *AgentSetConfig) (adk.Agent, error) {
	instruction := cfg.SystemPrompt
	if instruction == "" {
		instruction = buildInstruction(setCfg.PromptMgr, cfg.PromptName)
	}

	toolsCfg := adk.ToolsConfig{}
	toolsCfg.Tools = cfg.Tools

	agentConfig := &adk.ChatModelAgentConfig{
		Name:                cfg.Name,
		Description:         cfg.Description,
		Instruction:         instruction,
		Model:               setCfg.ChatModel,
		ToolsConfig:         toolsCfg,
		Handlers:            setCfg.Handlers,
		ModelFailoverConfig: buildFailoverConfig(setCfg.FailoverModels),
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries: 2,
			IsRetryAble: func(ctx context.Context, err error) bool {
				if err == nil {
					return false
				}
				// Don't retry cancellation or deadline exceeded.
				if ctx.Err() != nil {
					return false
				}
				return true
			},
		},
	}

	// Wire MessageModifier through GenModelInput. Default behavior:
	// prepend instruction as system message, then pass user messages.
	// The modifier injects memory context, profile facets, etc.
	if cfg.MessageModifier != nil {
		agentConfig.GenModelInput = func(ctx context.Context, instr string, input *adk.TypedAgentInput[*schema.Message]) ([]*schema.Message, error) {
			var msgs []*schema.Message
			if instr != "" {
				msgs = append(msgs, schema.SystemMessage(instr))
			}
			msgs = append(msgs, input.Messages...)
			return cfg.MessageModifier(ctx, msgs), nil
		}
	}

	return adk.NewChatModelAgent(ctx, agentConfig)
}

// buildInstruction renders the system prompt from the prompt manager.
// Returns an empty string when the manager is nil or the prompt is
// not found.
func buildInstruction(pm *prompts.Manager, name prompts.Name) string {
	if pm == nil {
		return ""
	}
	return pm.Get(name)
}

// filterToolsByAllowlist returns tools matching the given allowlist.
// An empty allowlist returns nil (no tools). A ["*"] allowlist returns
// all tools (used by general agent and tools_agent).
func filterToolsByAllowlist(tools []tool.BaseTool, allowlist []string) []tool.BaseTool {
	if len(tools) == 0 {
		return nil
	}
	if len(allowlist) == 0 {
		return nil
	}
	// "*" means all tools
	if len(allowlist) == 1 && allowlist[0] == "*" {
		out := make([]tool.BaseTool, len(tools))
		copy(out, tools)
		return out
	}
	allowed := make(map[string]bool, len(allowlist))
	for _, name := range allowlist {
		allowed[name] = true
	}
	out := make([]tool.BaseTool, 0, len(allowed))
	for _, t := range tools {
		name := toolName(t)
		if allowed[name] {
			out = append(out, t)
		}
	}
	return out
}

// buildFailoverConfig creates a ModelFailoverConfig from a list of backup
// models. When the primary model fails, the agent cycles through the failover
// list in order, trying each model once.
func buildFailoverConfig(models []model.ToolCallingChatModel) *adk.ModelFailoverConfig[*schema.Message] {
	if len(models) == 0 {
		return nil
	}

	// Capture the failover models in a closure so they can be cycled through
	// across multiple failover attempts.
	idx := 0
	return &adk.ModelFailoverConfig[*schema.Message]{
		MaxRetries: uint(len(models)),
		ShouldFailover: func(ctx context.Context, _ *schema.Message, err error) bool {
			if ctx.Err() != nil {
				return false // don't failover on cancel/deadline
			}
			return err != nil
		},
		GetFailoverModel: func(ctx context.Context, _ *adk.FailoverContext[*schema.Message]) (
			model.BaseChatModel, []*schema.Message, error) {
			if idx >= len(models) {
				return nil, nil, fmt.Errorf("eino: no more failover models available")
			}
			m := models[idx]
			idx++
			return m, nil, nil
		},
	}
}

// toolName returns the name of a tool by calling its Info method.
// Returns "" if Info fails (nil tool or Info error).
func toolName(t tool.BaseTool) string {
	if t == nil {
		return ""
	}
	info, err := t.Info(context.TODO())
	if err != nil || info == nil {
		return ""
	}
	return info.Name
}
