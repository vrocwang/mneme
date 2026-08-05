package boot

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/tools"
)

// registerConfigTools registers config introspection tools.
// Lives in boot (not config) so config doesn't have to import tools,
// which would create an import cycle when tools imports config.
func registerConfigTools(capReg *capability.CapabilityRegistry, cfg *config.Config) {
	capReg.RegisterTool("builtin", &configSnapshotTool{cfg})
	capReg.RegisterTool("builtin", &configAutonomyTool{cfg})
	capReg.RegisterTool("builtin", &configDataPathsTool{cfg})
}

// ── config_snapshot ────────────────────────────────────────────────────

type configSnapshotTool struct{ cfg *config.Config }

func (t *configSnapshotTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "config_snapshot",
		Description: "Returns a read-only snapshot of the current runtime configuration.",
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
}

func (t *configSnapshotTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	snapshot := map[string]interface{}{
		"agent": map[string]interface{}{
			"default_model":     t.cfg.Agent.DefaultModel,
			"max_output_tokens": t.cfg.Agent.MaxOutputTokens,
			"temperature":       t.cfg.Agent.Temperature,
		},
		"security": map[string]interface{}{
			"tier":           t.cfg.Security.Tier,
			"workspace_only": t.cfg.Security.WorkspaceOnly,
		},
		"workspace": t.cfg.Workspace,
		"memory": map[string]interface{}{
			"max_chunk_size": t.cfg.Memory.MaxChunkSize,
			"retention_days": t.cfg.Memory.RetentionDays,
		},
		"tools": map[string]interface{}{
			"max_output_bytes": t.cfg.Tools.Shell.MaxOutputBytes,
		},
	}
	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// ── config_autonomy ────────────────────────────────────────────────────

type configAutonomyTool struct{ cfg *config.Config }

func (t *configAutonomyTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "config_autonomy",
		Description: "Returns the current autonomy settings.",
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
}

func (t *configAutonomyTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	ac := t.cfg.Autonomy
	snapshot := map[string]interface{}{
		"level":                      ac.Level,
		"workspace_only":             ac.WorkspaceOnly,
		"allowed_commands":           ac.AllowedCommands,
		"forbidden_paths":            ac.ForbiddenPaths,
		"max_actions_per_hour":       ac.MaxActionsPerHour,
		"max_cost_per_day_cents":     ac.MaxCostPerDayCents,
		"require_task_plan_approval": ac.RequireTaskPlanApproval,
		"block_high_risk_commands":   ac.BlockHighRiskCommands,
	}
	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// ── config_data_paths ──────────────────────────────────────────────────

type configDataPathsTool struct{ cfg *config.Config }

func (t *configDataPathsTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "config_data_paths",
		Description: "Returns the filesystem paths the agent can use.",
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
}

func (t *configDataPathsTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	paths := map[string]interface{}{
		"workspace":  t.cfg.Workspace,
		"action_dir": t.cfg.Workspace,
	}
	b, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(b)}
}
