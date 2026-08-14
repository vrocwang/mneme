package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/tools"
)

// ── Cron list tool ──────────────────────────────────────────────────────────

type cronListTool struct {
	sched *Scheduler
}

func NewCronListTool(sched *Scheduler) tools.Tool {
	return &cronListTool{sched: sched}
}

func (t *cronListTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "cron_list",
		Description: "List all scheduled cron jobs with their status, next run time, and schedule.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

func (t *cronListTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	jobs := t.sched.List()
	type jobInfo struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Enabled  bool   `json:"enabled"`
		JobType  string `json:"job_type"`
		LastRun  string `json:"last_run,omitempty"`
		NextRun  string `json:"next_run,omitempty"`
	}
	infos := make([]jobInfo, 0, len(jobs))
	for _, j := range jobs {
		info := jobInfo{
			ID:       j.ID,
			Name:     j.Name,
			Schedule: j.Schedule,
			Enabled:  j.Enabled,
			JobType:  string(j.JobType),
		}
		if !j.LastRun.IsZero() {
			info.LastRun = j.LastRun.Format(time.RFC3339)
		}
		if !j.NextRun.IsZero() {
			info.NextRun = j.NextRun.Format(time.RFC3339)
		}
		infos = append(infos, info)
	}
	data, err := json.MarshalIndent(infos, "", "  ")
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("marshal jobs: %v", err)}
	}
	return tools.Result{Success: true, Output: string(data)}
}

// ── Cron add tool ───────────────────────────────────────────────────────────

type cronAddTool struct {
	sched *Scheduler
}

func NewCronAddTool(sched *Scheduler) tools.Tool {
	return &cronAddTool{sched: sched}
}

func (t *cronAddTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "cron_add",
		Description: "Schedule a new cron job. Returns the created job ID.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable job name",
				},
				"schedule": map[string]interface{}{
					"type":        "string",
					"description": "Schedule expression: simplified ('hourly', 'daily', 'weekly') or standard 5-field cron ('min hour dom month dow'). E.g. '0 9 * * 1-5' for weekdays at 9am.",
				},
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "The agent prompt to execute on each run (for agent-type jobs).",
				},
				"shell_command": map[string]interface{}{
					"type":        "string",
					"description": "Shell command to execute (for shell-type jobs). Mutually exclusive with prompt.",
				},
			},
			"required": []string{"name", "schedule"},
		},
	}
}

func (t *cronAddTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	name, _ := args["name"].(string)
	schedule, _ := args["schedule"].(string)
	prompt, _ := args["prompt"].(string)
	shellCmd, _ := args["shell_command"].(string)

	if name == "" && schedule == "" {
		return tools.Result{Error: "missing both 'name' and 'schedule'. You MUST provide a name (e.g. 'screenshot_10_30') and a schedule expression (e.g. '30 10 * * *')."}
	}
	if name == "" {
		return tools.Result{Error: "missing 'name'. Provide a short descriptive name like 'morning_screenshot'."}
	}
	if schedule == "" {
		return tools.Result{Error: "missing 'schedule'. Use a 5-field cron expression like '30 10 * * *' for 10:30 AM daily, or a simplified one: 'hourly', 'daily', 'weekly'."}
	}

	if prompt == "" && shellCmd == "" {
		return tools.Result{Error: "at least one of 'prompt' or 'shell_command' must be provided"}
	}

	id := slugify(name) + "_" + fmt.Sprint(time.Now().UnixMilli()%10000)
	jobType := JobTypeAgent
	if shellCmd != "" {
		jobType = JobTypeShell
	}

	if jobType == JobTypeAgent {
		if prompt == "" {
			return tools.Result{Error: "prompt is required for agent-type jobs"}
		}
		if t.sched.sendFn == nil {
			return tools.Result{Error: "agent chat sender is not configured; agent-type cron jobs are unavailable"}
		}
	}

	job := &Job{
		ID:           id,
		Name:         name,
		Schedule:     schedule,
		Enabled:      true,
		JobType:      jobType,
		AgentPrompt:  prompt,
		ShellCommand: shellCmd,
	}
	// Set handler for agent-type jobs via the scheduler's ChatSender.
	if jobType == JobTypeAgent && prompt != "" {
		if sendFn := t.sched.sendFn; sendFn != nil {
			p := prompt
			job.Handler = func(ctx context.Context) error {
				_, err := sendFn(ctx, p)
				return err
			}
		}
	}
	t.sched.Add(job)

	return tools.Result{Success: true, Output: fmt.Sprintf("Job created: %s (schedule: %s)", id, schedule)}
}

// ── Cron remove tool ────────────────────────────────────────────────────────

type cronRemoveTool struct {
	sched *Scheduler
}

func NewCronRemoveTool(sched *Scheduler) tools.Tool {
	return &cronRemoveTool{sched: sched}
}

func (t *cronRemoveTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "cron_remove",
		Description: "Remove a scheduled cron job by ID.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the cron job to remove",
				},
			},
			"required": []string{"job_id"},
		},
	}
}

func (t *cronRemoveTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	jobID, _ := args["job_id"].(string)
	if jobID == "" {
		return tools.Result{Error: "job_id is required"}
	}
	exists := false
	for _, j := range t.sched.List() {
		if j.ID == jobID {
			exists = true
			break
		}
	}
	if !exists {
		return tools.Result{Error: fmt.Sprintf("job %s not found", jobID)}
	}
	t.sched.Remove(jobID)
	return tools.Result{Success: true, Output: fmt.Sprintf("Job %s removed", jobID)}
}

// ── Cron run tool ───────────────────────────────────────────────────────────

type cronRunTool struct {
	sched *Scheduler
}

func NewCronRunTool(sched *Scheduler) tools.Tool {
	return &cronRunTool{sched: sched}
}

func (t *cronRunTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "cron_run",
		Description: "Trigger a cron job immediately, regardless of its schedule.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the cron job to run now",
				},
			},
			"required": []string{"job_id"},
		},
	}
}

func (t *cronRunTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	jobID, _ := args["job_id"].(string)
	if jobID == "" {
		return tools.Result{Error: "job_id is required"}
	}
	if err := t.sched.Run(jobID); err != nil {
		return tools.Result{Error: err.Error()}
	}
	return tools.Result{Success: true, Output: fmt.Sprintf("Job %s triggered", jobID)}
}

// ── Cron recent runs tool ───────────────────────────────────────────────────

type cronRunsTool struct {
	sched *Scheduler
}

func NewCronRunsTool(sched *Scheduler) tools.Tool {
	return &cronRunsTool{sched: sched}
}

func (t *cronRunsTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "cron_runs",
		Description: "Show recent run history for all or a specific cron job.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional job ID to filter. Leave empty for all jobs.",
				},
			},
		},
	}
}

func (t *cronRunsTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	jobID, _ := args["job_id"].(string)
	jobs := t.sched.List()

	var lines []string
	for _, j := range jobs {
		if jobID != "" && j.ID != jobID {
			continue
		}
		lastRun := "never"
		if !j.LastRun.IsZero() {
			lastRun = j.LastRun.Format(time.RFC3339)
		}
		nextRun := "not scheduled"
		if !j.NextRun.IsZero() {
			nextRun = j.NextRun.Format(time.RFC3339)
		}
		lines = append(lines, fmt.Sprintf("%s (%s): last=%s next=%s enabled=%v",
			j.ID, j.Schedule, lastRun, nextRun, j.Enabled))
	}
	if len(lines) == 0 {
		if jobID != "" {
			return tools.Result{Error: fmt.Sprintf("job %s not found", jobID)}
		}
		return tools.Result{Success: true, Output: "No cron jobs registered."}
	}
	return tools.Result{Success: true, Output: strings.Join(lines, "\n")}
}

// ── Helper ──────────────────────────────────────────────────────────────────

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "_")
}

// RegisterTools registers all cron tools with the capability registry under
// the "cron" set.
func RegisterTools(reg *capability.CapabilityRegistry, sched *Scheduler) {
	reg.EnsureSet(&capability.CapabilitySet{
		ID:      "cron",
		Name:    "Cron",
		Kind:    capability.KindBuiltin,
		Enabled: true,
	})
	reg.RegisterTool("cron", NewCronListTool(sched))
	reg.RegisterTool("cron", NewCronAddTool(sched))
	reg.RegisterTool("cron", NewCronRemoveTool(sched))
	reg.RegisterTool("cron", NewCronRunTool(sched))
	reg.RegisterTool("cron", NewCronRunsTool(sched))
}
