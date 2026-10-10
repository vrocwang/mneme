package agent

import (
	"fmt"

	agenttoml "github.com/simon/mneme/internal/agent/toml"
	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/tools"
	pkgtools "github.com/simon/mneme/pkg/tools"
)

// defaultAgentTier is the tier recorded for an agent created without one.
const defaultAgentTier = "worker"

// AgentRPC exposes agent management methods bound at go.agent.AgentRPC.
type AgentRPC struct {
	reg       *capability.CapabilityRegistry
	agentsDir string
}

// NewAgentRPC creates an AgentRPC for Wails binding.
func NewAgentRPC(reg *capability.CapabilityRegistry, agentsDir string) *AgentRPC {
	return &AgentRPC{reg: reg, agentsDir: agentsDir}
}

// SetAgentRegistry updates the capability registry reference (called after
// startup). The name is struct-qualified because Wails binds every exported
// method and a bare SetRegistry would collide with capability.CapabilityRPC's.
func (a *AgentRPC) SetAgentRegistry(reg *capability.CapabilityRegistry) {
	a.reg = reg
}

// SetAgentsDir sets the directory user-defined agents are persisted to
// (workspace/agents). Without it, create/update/delete only touch the
// in-memory registry and are lost on restart.
func (a *AgentRPC) SetAgentsDir(dir string) {
	a.agentsDir = dir
}

// ListAgents returns all registered agents (excluding hidden).
func (a *AgentRPC) ListAgents() []map[string]interface{} {
	if a.reg == nil {
		return nil
	}
	descs := a.reg.AllAgents()
	result := make([]map[string]interface{}, 0, len(descs))
	for _, d := range descs {
		if d.Hidden {
			continue
		}
		result = append(result, map[string]interface{}{
			"id":          d.ID,
			"name":        d.Name,
			"description": d.Description,
		})
	}
	return result
}

// UpsertAgent creates or updates a user-defined agent. Persisted to
// workspace/agents/<id>.toml and registered in-memory immediately.
// tier, model, maxIterations, and toolAllowlist come from the agent editor;
// zero values fall back to sensible defaults.
func (a *AgentRPC) UpsertAgent(id, name, description, systemPrompt, tier, model string, maxIterations int, toolAllowlist []string) error {
	if a.reg == nil {
		return fmt.Errorf("capability registry not available")
	}
	if id == "" || name == "" {
		return fmt.Errorf("id and name are required")
	}

	// Remove existing agent with same ID first.
	if _, ok := a.reg.GetAgent(id); ok {
		a.reg.UnregisterAgent(id)
	}

	if tier == "" {
		tier = defaultAgentTier
	}
	if maxIterations <= 0 {
		maxIterations = pkgtools.DefaultMaxIterations
	}
	if len(toolAllowlist) == 0 {
		toolAllowlist = []string{"*"}
	}

	def := &tools.AgentDef{
		ID:            id,
		Name:          name,
		Description:   description,
		Tier:          tier,
		MaxIterations: maxIterations,
		SystemPrompt:  systemPrompt,
		ToolAllowlist: toolAllowlist,
	}
	if model != "" {
		def.Model = model
	}

	// Persist to TOML so it survives restarts. An unset directory is a wiring
	// bug, not a silent "memory-only" mode: the agent would vanish on restart.
	if a.agentsDir == "" {
		return fmt.Errorf("agents directory not configured")
	}
	if err := agenttoml.SaveAgentToFile(a.agentsDir, def); err != nil {
		return fmt.Errorf("save agent: %w", err)
	}

	a.reg.RegisterAgentSet(capability.UserAgentsSetID, capability.KindUserAgent, capability.UserAgentsSetName, def)
	return nil
}

// RemoveAgent unregisters a user-defined agent and deletes its TOML file.
func (a *AgentRPC) RemoveAgent(id string) error {
	if a.reg == nil {
		return fmt.Errorf("capability registry not available")
	}
	if id == "" {
		return fmt.Errorf("id is required")
	}
	if _, ok := a.reg.GetAgent(id); !ok {
		return fmt.Errorf("agent %q not found", id)
	}
	if a.agentsDir == "" {
		return fmt.Errorf("agents directory not configured")
	}
	if err := agenttoml.DeleteAgentFile(a.agentsDir, id); err != nil {
		return fmt.Errorf("delete agent file: %w", err)
	}
	a.reg.UnregisterAgent(id)
	return nil
}
