package toml

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	btoml "github.com/BurntSushi/toml"
	"github.com/simon/mneme/internal/tools"
)

// AgentTOML is the on-disk format for agent definitions.
type AgentTOML struct {
	ID            string              `toml:"id"`
	Name          string              `toml:"name"`
	Description   string              `toml:"description"`
	Tier          string              `toml:"tier"`
	Model         string              `toml:"model,omitempty"`
	SystemPrompt  string              `toml:"system_prompt,omitempty"`
	ToolAllowlist []string            `toml:"tool_allowlist,omitempty"`
	ToolDenylist  []string            `toml:"tool_denylist,omitempty"`
	MaxIterations int                 `toml:"max_iterations"`
	Hidden        bool                `toml:"hidden"`
	Background    bool                `toml:"background"`
	SandboxMode   string              `toml:"sandbox_mode,omitempty"`
	Temperature   float64             `toml:"temperature,omitempty"`
	TimeoutSecs   int                 `toml:"timeout_secs,omitempty"`
	SubagentRefs  []AgentTOMLSubagent `toml:"subagents,omitempty"`
}

type AgentTOMLSubagent struct {
	AgentID      string `toml:"agent_id"`
	SkillsFilter string `toml:"skills_filter,omitempty"`
}

// LoadAgentsFromDir reads all *.toml files from agentsDir and returns parsed
// agent definitions. Files that fail to parse are returned as errors alongside
// successfully parsed definitions.
//
// This scans flat *.toml files directly in the directory. For agent packs
// organised as subdirectories with agent.toml + prompt.md, use
// LoadAgentPacksFromDir.
func LoadAgentsFromDir(agentsDir string) ([]*tools.AgentDef, []error) {
	if _, err := os.Stat(agentsDir); os.IsNotExist(err) {
		return nil, nil
	}
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, []error{fmt.Errorf("read agents dir: %w", err)}
	}
	var defs []*tools.AgentDef
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		path := filepath.Join(agentsDir, entry.Name())
		var a AgentTOML
		if _, err := btoml.DecodeFile(path, &a); err != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", path, err))
			continue
		}
		if a.ID == "" || a.Name == "" {
			errs = append(errs, fmt.Errorf("skip %s: missing id or name", path))
			continue
		}
		defs = append(defs, agentTOMLToDef(a))
	}
	return defs, errs
}

// agentTOMLToDef converts a parsed AgentTOML struct into a tools.AgentDef,
// applying defaults for missing fields. Shared by LoadAgentsFromDir and
// LoadAgentPacksFromDir to eliminate duplication.
func agentTOMLToDef(a AgentTOML) *tools.AgentDef {
	if a.MaxIterations <= 0 {
		a.MaxIterations = 10
	}
	if a.Tier == "" {
		a.Tier = "worker"
	}
	def := &tools.AgentDef{
		ID: a.ID, Name: a.Name, Description: a.Description,
		Tier: a.Tier, Model: a.Model, SystemPrompt: a.SystemPrompt,
		ToolAllowlist: a.ToolAllowlist, ToolDenylist: a.ToolDenylist,
		MaxIterations: a.MaxIterations, Hidden: a.Hidden,
		Background: a.Background, SandboxMode: a.SandboxMode,
		Temperature: a.Temperature, TimeoutSecs: a.TimeoutSecs,
	}
	for _, sub := range a.SubagentRefs {
		def.SubagentRefs = append(def.SubagentRefs, tools.SubagentRef{
			AgentID: sub.AgentID, SkillsFilter: sub.SkillsFilter,
		})
	}
	return def
}

// LoadAgentPacksFromDir scans subdirectories of packsDir for agent.toml files
// and returns parsed AgentDefs. Each subdirectory may also contain a
// prompt.md that is loaded as the agent's system prompt.
//
// This complements LoadAgentsFromDir (flat *.toml files) by supporting
// agent packs that bundle agent.toml + prompt.md in a subdirectory.
func LoadAgentPacksFromDir(packsDir string) ([]*tools.AgentDef, []error) {
	if _, err := os.Stat(packsDir); os.IsNotExist(err) {
		return nil, nil
	}
	entries, err := os.ReadDir(packsDir)
	if err != nil {
		return nil, []error{fmt.Errorf("read agent packs dir: %w", err)}
	}
	var defs []*tools.AgentDef
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		packDir := filepath.Join(packsDir, entry.Name())
		tomlPath := filepath.Join(packDir, "agent.toml")
		if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
			continue
		}
		var a AgentTOML
		if _, err := btoml.DecodeFile(tomlPath, &a); err != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", tomlPath, err))
			continue
		}
		if a.ID == "" || a.Name == "" {
			errs = append(errs, fmt.Errorf("skip %s: missing id or name", tomlPath))
			continue
		}
		// Load prompt.md if present.
		if a.SystemPrompt == "" {
			mdPath := filepath.Join(packDir, "prompt.md")
			if data, err := os.ReadFile(mdPath); err == nil {
				a.SystemPrompt = string(data)
			}
		}
		defs = append(defs, agentTOMLToDef(a))
	}
	return defs, errs
}

// SaveAgentToFile writes an agent definition to a TOML file.
func SaveAgentToFile(agentsDir string, def *tools.AgentDef) error {
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("create agents dir: %w", err)
	}
	a := AgentTOML{
		ID: def.ID, Name: def.Name, Description: def.Description,
		Tier: def.Tier, Model: def.Model, SystemPrompt: def.SystemPrompt,
		ToolAllowlist: def.ToolAllowlist, ToolDenylist: def.ToolDenylist,
		MaxIterations: def.MaxIterations, Hidden: def.Hidden,
		Background: def.Background, SandboxMode: def.SandboxMode,
		Temperature: def.Temperature, TimeoutSecs: def.TimeoutSecs,
	}
	for _, sub := range def.SubagentRefs {
		a.SubagentRefs = append(a.SubagentRefs, AgentTOMLSubagent{
			AgentID: sub.AgentID, SkillsFilter: sub.SkillsFilter,
		})
	}
	path := filepath.Join(agentsDir, def.ID+".toml")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create agent file: %w", err)
	}
	defer f.Close()
	if err := btoml.NewEncoder(f).Encode(a); err != nil {
		return fmt.Errorf("encode agent: %w", err)
	}
	return nil
}

// DeleteAgentFile removes an agent TOML file by agent ID.
func DeleteAgentFile(agentsDir, agentID string) error {
	path := filepath.Join(agentsDir, agentID+".toml")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove agent file: %w", err)
	}
	return nil
}
