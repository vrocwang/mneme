package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/simon/mneme/internal/tools"
)

// ── Monitor start tool ─────────────────────────────────────────────────────

type monitorStartTool struct {
	mgr *Manager
}

func NewMonitorStartTool(mgr *Manager) tools.Tool {
	return &monitorStartTool{mgr: mgr}
}

func (t *monitorStartTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "monitor_start",
		Description: "Start a background command and return a monitor ID. Use monitor_read to check output and status later. Default timeout: 5 minutes.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Shell command to run in the background",
				},
				"timeout_secs": map[string]interface{}{
					"type":        "integer",
					"description": "Timeout in seconds (default: 300 = 5 minutes)",
				},
				"use_pty": map[string]interface{}{
					"type":        "boolean",
					"description": "Allocate a pseudo-terminal for the command. Required for interactive CLI tools that need a TTY (OAuth flows, progress bars, etc.). Default: false.",
				},
			},
			"required": []string{"command"},
		},
	}
}

func (t *monitorStartTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	if t.mgr == nil {
		return tools.Result{Error: "monitor manager not initialized"}
	}
	command, _ := args["command"].(string)
	if command == "" {
		return tools.Result{Error: "command is required"}
	}
	timeout := 300 * time.Second
	if n, ok := args["timeout_secs"].(float64); ok && n > 0 {
		timeout = time.Duration(n) * time.Second
	}
	usePty, _ := args["use_pty"].(bool)

	id, err := t.mgr.Start(ctx, command, timeout, usePty)
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	return tools.Result{Success: true, Output: fmt.Sprintf("Monitor started: %s (timeout: %s)", id, timeout)}
}

// ── Monitor list tool ──────────────────────────────────────────────────────

type monitorListTool struct {
	mgr *Manager
}

func NewMonitorListTool(mgr *Manager) tools.Tool {
	return &monitorListTool{mgr: mgr}
}

func (t *monitorListTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "monitor_list",
		Description: "List all active and recently completed monitor runs.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

func (t *monitorListTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	if t.mgr == nil {
		return tools.Result{Error: "monitor manager not initialized"}
	}
	runs := t.mgr.List()
	type info struct {
		ID        string `json:"id"`
		Command   string `json:"command"`
		Status    string `json:"status"`
		ExitCode  int    `json:"exit_code"`
		StartedAt string `json:"started_at"`
		Error     string `json:"error,omitempty"`
	}
	infos := make([]info, 0, len(runs))
	for _, r := range runs {
		infos = append(infos, info{
			ID: r.ID, Command: r.Command, Status: string(r.Status),
			ExitCode: r.ExitCode, StartedAt: r.StartedAt.Format(time.RFC3339),
			Error: r.Error,
		})
	}
	data, err := json.MarshalIndent(infos, "", "  ")
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal: %v", err)}
	}
	return tools.Result{Success: true, Output: string(data)}
}

// ── Monitor read tool ──────────────────────────────────────────────────────

type monitorReadTool struct {
	mgr *Manager
}

func NewMonitorReadTool(mgr *Manager) tools.Tool {
	return &monitorReadTool{mgr: mgr}
}

func (t *monitorReadTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "monitor_read",
		Description: "Read the output of a monitor run. Returns current output for running monitors or final output for completed ones.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"monitor_id": map[string]interface{}{
					"type":        "string",
					"description": "Monitor ID returned by monitor_start",
				},
			},
			"required": []string{"monitor_id"},
		},
	}
}

func (t *monitorReadTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	if t.mgr == nil {
		return tools.Result{Error: "monitor manager not initialized"}
	}
	id, _ := args["monitor_id"].(string)
	if id == "" {
		runs := t.mgr.List()
		if len(runs) > 0 {
			var ids []string
			for _, r := range runs {
				ids = append(ids, fmt.Sprintf("%s (%s)", r.ID, r.Status))
			}
			return tools.Result{Error: fmt.Sprintf(
				"monitor_id is required. Known monitors: %s", strings.Join(ids, ", "))}
		}
		return tools.Result{Error: "monitor_id is required — no active monitors. Use monitor_start to start a background command."}
	}

	run, err := t.mgr.Get(id)
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	output, err := t.mgr.ReadOutput(id)
	if err != nil {
		return tools.Result{Error: err.Error()}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Status: %s\n", run.Status))
	if run.Error != "" {
		b.WriteString(fmt.Sprintf("Error: %s\n", run.Error))
	}
	b.WriteString(fmt.Sprintf("Duration: %s\n", time.Since(run.StartedAt).Round(time.Second)))
	b.WriteString(fmt.Sprintf("Output:\n%s", output))
	return tools.Result{Success: true, Output: b.String()}
}

// ── Monitor stop tool ──────────────────────────────────────────────────────

type monitorStopTool struct {
	mgr *Manager
}

func NewMonitorStopTool(mgr *Manager) tools.Tool {
	return &monitorStopTool{mgr: mgr}
}

func (t *monitorStopTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "monitor_stop",
		Description: "Stop a running monitor by ID.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"monitor_id": map[string]interface{}{
					"type":        "string",
					"description": "Monitor ID to stop",
				},
			},
			"required": []string{"monitor_id"},
		},
	}
}

func (t *monitorStopTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	if t.mgr == nil {
		return tools.Result{Error: "monitor manager not initialized"}
	}
	id, _ := args["monitor_id"].(string)
	if id == "" {
		return tools.Result{Error: "monitor_id is required"}
	}
	if err := t.mgr.Stop(id); err != nil {
		return tools.Result{Error: err.Error()}
	}
	return tools.Result{Success: true, Output: fmt.Sprintf("Monitor %s stopped", id)}
}
