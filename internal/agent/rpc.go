package agent

import (
	"fmt"

	agenttoml "github.com/simon/mneme/internal/agent/toml"
	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/tools"
)

// AgentRPC exposes agent management methods bound at go.agent.AgentRPC.
type AgentRPC struct {
	reg       *capability.CapabilityRegistry
	agentsDir string
}

// NewAgentRPC creates an AgentRPC for Wails binding.
func NewAgentRPC(reg *capability.CapabilityRegistry, agentsDir string) *AgentRPC {
	return &AgentRPC{reg: reg, agentsDir: agentsDir}
}

// SetRegistry updates the capability registry reference (called after startup).
func (a *AgentRPC) SetRegistry(reg *capability.CapabilityRegistry) {
	a.reg = reg
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
func (a *AgentRPC) UpsertAgent(id, name, description, systemPrompt string) error {
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

	def := &tools.AgentDef{
		ID:            id,
		Name:          name,
		Description:   description,
		Tier:          "worker",
		MaxIterations: 10,
		SystemPrompt:  systemPrompt,
		ToolAllowlist: []string{"*"},
	}

	// Persist to TOML so it survives restarts.
	if a.agentsDir != "" {
		if err := agenttoml.SaveAgentToFile(a.agentsDir, def); err != nil {
			return fmt.Errorf("save agent: %w", err)
		}
	}

	a.reg.RegisterAgent("builtin", def)
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
	a.reg.UnregisterAgent(id)

	if a.agentsDir != "" {
		if err := agenttoml.DeleteAgentFile(a.agentsDir, id); err != nil {
			return fmt.Errorf("delete agent file: %w", err)
		}
	}
	return nil
}
