// Skill Runtime extension for Mneme.
//
// Provides skill execution tools:
//   - skill_run: execute a skill by name
//   - skill_cancel: cancel a running skill
//   - skill_status: get skill execution status
//   - skill_list: list installed and running skills
//   - skill_logs: retrieve skill execution logs
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

// dataDir returns the host workspace directory.
func dataDir() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "data")
}

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "skill-runtime",
		Version:     "0.1.0",
		Description: "Skill execution runtime: run, cancel, status, list, logs",
	})
	// Skill execution can legitimately run long; match the original 5-minute
	// per-call timeout rather than the SDK's 60s default.
	srv.SetCallTimeout(5 * time.Minute)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "skill_run",
		Description: "Run a skill by name. Skills are executable scripts or binaries. Returns a run ID for tracking.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"skillName": map[string]interface{}{"type": "string", "description": "Skill name to execute"},
				"args":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Arguments to pass to the skill"},
				"workDir":   map[string]interface{}{"type": "string", "description": "Working directory for skill execution"},
				"env":       map[string]interface{}{"type": "object", "description": "Environment variables"},
				"timeoutMs": map[string]interface{}{"type": "number", "description": "Timeout in milliseconds (default 60000)"},
			},
			"required": []string{"skillName"},
		},
		Permission: "execute",
		HasEffects: true,
	}, skillRunCmd)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "skill_cancel",
		Description: "Cancel a running skill by its run ID",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"runId":  map[string]interface{}{"type": "string", "description": "Run ID from skill_run"},
				"signal": map[string]interface{}{"type": "string", "description": "Signal to send (SIGTERM/SIGKILL, default SIGTERM)"},
			},
			"required": []string{"runId"},
		},
		Permission: "execute",
		HasEffects: true,
	}, func(ctx context.Context, args map[string]interface{}) extsdk.Result {
		return skillCancelCmd(args)
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "skill_status",
		Description: "Get the status of a running or completed skill by run ID",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"runId": map[string]interface{}{"type": "string", "description": "Run ID from skill_run. Omit to list all."},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, func(ctx context.Context, args map[string]interface{}) extsdk.Result {
		return skillStatus(args)
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "skill_list",
		Description: "List all installed skills available for execution",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"searchPath": map[string]interface{}{"type": "string", "description": "Comma-separated paths to search for skills"},
				"filter":     map[string]interface{}{"type": "string", "description": "Optional name filter (substring match)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, func(ctx context.Context, args map[string]interface{}) extsdk.Result {
		return skillList(args)
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "skill_logs",
		Description: "Get execution logs for a skill run",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"runId":         map[string]interface{}{"type": "string", "description": "Run ID from skill_run"},
				"tail":          map[string]interface{}{"type": "number", "description": "Number of last lines to return (default 100)"},
				"includeStderr": map[string]interface{}{"type": "boolean", "description": "Include stderr in output (default true)"},
			},
			"required": []string{"runId"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, func(ctx context.Context, args map[string]interface{}) extsdk.Result {
		return skillLogs(args)
	})

	srv.RegisterAgent(extsdk.AgentDef{
		ID:          "skill_executor",
		Name:        "Skill Executor",
		Description: "Executes skills as subprocesses, monitors their output, and manages skill lifecycle",
		Tier:        "worker",
		SystemPrompt: `You are a skill execution specialist. Your role is to run skills, monitor their progress, and handle errors.
- Start skills with appropriate arguments
- Monitor skill output for errors
- Cancel skills that hang or produce incorrect results
- Keep logs of skill execution for debugging`,
		ToolAllowlist: []string{"skill_run", "skill_cancel", "skill_status", "skill_list", "skill_logs", "shell", "read_file", "list_dir"},
		MaxIterations: 12,
		Hidden:        false,
	})

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "skill-runtime: %v\n", err)
		os.Exit(1)
	}
}

// ── Skill runtime state ───────────────────────────────────────────

type skillRun struct {
	ID        string    `json:"id"`
	SkillName string    `json:"skillName"`
	Status    string    `json:"status"` // running, completed, failed, cancelled
	StartTime time.Time `json:"startTime"`
	EndTime   time.Time `json:"endTime,omitempty"`
	ExitCode  int       `json:"exitCode"`
	Logs      []string  `json:"-"`
	Stdout    string    `json:"stdout,omitempty"`
	Stderr    string    `json:"stderr,omitempty"`
	cmd       *exec.Cmd
	cancel    context.CancelFunc
}

var (
	runs   = make(map[string]*skillRun)
	runsMu sync.Mutex
	runSeq int64
)

func skillRunCmd(ctx context.Context, args map[string]interface{}) extsdk.Result {
	skillName, _ := args["skillName"].(string)
	if skillName == "" {
		return extsdk.Result{Error: "skillName is required"}
	}

	workDir, _ := args["workDir"].(string)
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	timeoutMs := 60000
	if t, ok := intFromOptArgs(args, "timeoutMs"); ok && t > 0 {
		timeoutMs = t
	}

	// Resolve skill path
	skillPath := resolveSkillPath(skillName, workDir)
	if skillPath == "" {
		return extsdk.Result{Error: fmt.Sprintf("skill not found: %s", skillName)}
	}

	runCtx, runCancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)

	var skillArgs []string
	if arr, ok := args["args"].([]interface{}); ok {
		for _, a := range arr {
			if s, ok := a.(string); ok {
				skillArgs = append(skillArgs, s)
			}
		}
	}

	cmd := exec.CommandContext(runCtx, skillPath, skillArgs...)
	cmd.Dir = workDir

	if envMap, ok := args["env"].(map[string]interface{}); ok {
		cmd.Env = os.Environ()
		for k, v := range envMap {
			if vs, ok := v.(string); ok {
				cmd.Env = append(cmd.Env, k+"="+vs)
			}
		}
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		runCancel()
		return extsdk.Result{Error: fmt.Sprintf("stdout pipe: %v", err)}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		runCancel()
		return extsdk.Result{Error: fmt.Sprintf("stderr pipe: %v", err)}
	}

	if err := cmd.Start(); err != nil {
		runCancel()
		return extsdk.Result{Error: fmt.Sprintf("skill start: %v", err)}
	}

	runsMu.Lock()
	runSeq++
	runID := fmt.Sprintf("run_%d_%s", runSeq, skillName)
	run := &skillRun{
		ID:        runID,
		SkillName: skillName,
		Status:    "running",
		StartTime: time.Now(),
		cmd:       cmd,
		cancel:    runCancel,
	}
	runs[runID] = run
	runsMu.Unlock()

	go func() {
		outBytes, _ := io.ReadAll(stdout)
		errBytes, _ := io.ReadAll(stderr)
		err := cmd.Wait()

		runsMu.Lock()
		run.Stdout = string(outBytes)
		run.Stderr = string(errBytes)
		run.EndTime = time.Now()
		if err != nil {
			if runCtx.Err() != nil {
				run.Status = "cancelled"
			} else {
				run.Status = "failed"
				if exitErr, ok := err.(*exec.ExitError); ok {
					run.ExitCode = exitErr.ExitCode()
				}
			}
		} else {
			run.Status = "completed"
		}
		runsMu.Unlock()
	}()

	return extsdk.Result{Success: true, Output: fmt.Sprintf("Skill started: %s\nRun ID: %s\nStatus: running\nTimeout: %dms", skillName, runID, timeoutMs)}
}

func skillCancelCmd(args map[string]interface{}) extsdk.Result {
	runID, _ := args["runId"].(string)
	if runID == "" {
		return extsdk.Result{Error: "runId is required"}
	}

	runsMu.Lock()
	run, ok := runs[runID]
	if !ok {
		runsMu.Unlock()
		return extsdk.Result{Error: fmt.Sprintf("run not found: %s", runID)}
	}
	if run.Status != "running" {
		runsMu.Unlock()
		return extsdk.Result{Success: true, Output: fmt.Sprintf("Run %s already %s", runID, run.Status)}
	}

	run.cancel()
	if run.cmd.Process != nil {
		run.cmd.Process.Signal(os.Kill)
	}
	run.Status = "cancelled"
	runsMu.Unlock()

	return extsdk.Result{Success: true, Output: fmt.Sprintf("Cancelled: %s", runID)}
}

func skillStatus(args map[string]interface{}) extsdk.Result {
	runID, _ := args["runId"].(string)

	runsMu.Lock()
	defer runsMu.Unlock()

	if runID != "" {
		run, ok := runs[runID]
		if !ok {
			return extsdk.Result{Error: fmt.Sprintf("run not found: %s", runID)}
		}
		b, _ := json.MarshalIndent(map[string]interface{}{
			"id":        run.ID,
			"skillName": run.SkillName,
			"status":    run.Status,
			"startTime": run.StartTime.Format(time.RFC3339),
			"endTime":   run.EndTime.Format(time.RFC3339),
			"exitCode":  run.ExitCode,
		}, "", "  ")
		return extsdk.Result{Success: true, Output: string(b)}
	}

	// List all
	var statuses []map[string]interface{}
	for _, r := range runs {
		statuses = append(statuses, map[string]interface{}{
			"id": r.ID, "skillName": r.SkillName, "status": r.Status,
		})
	}
	b, _ := json.MarshalIndent(statuses, "", "  ")
	return extsdk.Result{Success: true, Output: string(b)}
}

func skillList(args map[string]interface{}) extsdk.Result {
	searchPath, _ := args["searchPath"].(string)
	filter, _ := args["filter"].(string)

	paths := []string{"/usr/local/bin", filepath.Join(dataDir(), "skills")}
	if searchPath != "" {
		paths = splitAndTrim(searchPath, ",")
	}

	var found []map[string]string
	for _, dir := range paths {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || e.Name()[0] == '.' {
				continue
			}
			if filter != "" && !contains(e.Name(), filter) {
				continue
			}
			info, _ := e.Info()
			if info.Mode()&0111 == 0 {
				continue
			}
			found = append(found, map[string]string{
				"name": e.Name(),
				"path": dir + "/" + e.Name(),
				"size": fmt.Sprintf("%d", info.Size()),
			})
		}
	}

	b, _ := json.MarshalIndent(found, "", "  ")
	if len(found) == 0 {
		return extsdk.Result{Success: true, Output: fmt.Sprintf("No skills found. Place executables in %s/", filepath.Join(dataDir(), "skills"))}
	}
	return extsdk.Result{Success: true, Output: string(b)}
}

func skillLogs(args map[string]interface{}) extsdk.Result {
	runID, _ := args["runId"].(string)
	tail := 100
	if t, ok := intFromOptArgs(args, "tail"); ok && t > 0 {
		tail = t
	}
	includeStderr := true
	if is, ok := args["includeStderr"].(bool); ok {
		includeStderr = is
	}

	runsMu.Lock()
	run, ok := runs[runID]
	runsMu.Unlock()
	if !ok {
		return extsdk.Result{Error: fmt.Sprintf("run not found: %s", runID)}
	}

	var out string
	if run.Stdout != "" {
		out += fmt.Sprintf("--- STDOUT ---\n%s", tailStr(run.Stdout, tail))
	}
	if includeStderr && run.Stderr != "" {
		out += fmt.Sprintf("\n--- STDERR ---\n%s", tailStr(run.Stderr, tail))
	}
	if out == "" {
		out = fmt.Sprintf("No output yet. Status: %s", run.Status)
	}
	return extsdk.Result{Success: true, Output: out}
}

// ── Helpers ──────────────────────────────────────────────────────

func intFromOptArgs(args map[string]interface{}, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

func resolveSkillPath(name, workDir string) string {
	// Check exact path first
	if _, err := os.Stat(name); err == nil {
		return name
	}
	// Check common skill dirs
	dirs := []string{
		filepath.Join(dataDir(), "skills"),
		"/usr/local/bin",
		workDir,
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if info, err := os.Stat(p); err == nil && info.Mode()&0111 != 0 {
			return p
		}
	}
	// Check PATH
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

func tailStr(s string, n int) string {
	lines := splitLines(s)
	if len(lines) <= n {
		return s
	}
	result := ""
	for i := len(lines) - n; i < len(lines); i++ {
		result += lines[i] + "\n"
	}
	return result
}

func splitLines(s string) []string {
	var lines []string
	line := ""
	for _, c := range s {
		if c == '\n' {
			lines = append(lines, line)
			line = ""
		} else {
			line += string(c)
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func splitAndTrim(s, sep string) []string {
	var res []string
	for _, p := range strings.Split(s, sep) {
		p = trim(p)
		if p != "" {
			res = append(res, p)
		}
	}
	return res
}

func trim(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == ',') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == ',') {
		end--
	}
	return s[start:end]
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
