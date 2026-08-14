package tools

import (
	"context"
	"fmt"
	"strings"
)

// GitOps provides git operations through the agent.
type GitOps struct {
	workspaceRoot string
}

// NewGitOps creates a git operations tool.
func NewGitOps(workspaceRoot string) *GitOps {
	return &GitOps{workspaceRoot: workspaceRoot}
}

func (t *GitOps) Schema() Schema {
	return Schema{
		Name:        "git",
		Description: "Run git commands: status, diff, log, branch, add, commit",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Git subcommand: status, diff, log, branch, add, commit",
					"enum":        []string{"status", "diff", "log", "branch", "add", "commit", "show", "stash"},
				},
				"args": map[string]interface{}{
					"type":        "string",
					"description": "Additional arguments for the git command",
				},
			},
			"required": []string{"command"},
		},
	}
}

func (t *GitOps) PermissionLevel() PermissionLevel { return PermExecute }
func (t *GitOps) SideEffects() bool                { return true }

func (t *GitOps) Execute(ctx context.Context, args map[string]interface{}) Result {
	command, _ := args["command"].(string)
	extraArgs, _ := args["args"].(string)

	if command == "" {
		return Result{Error: "command is required (status, diff, log, branch, add, commit)"}
	}

	// Whitelist of allowed git commands
	allowed := map[string]bool{
		"status": true, "diff": true, "log": true, "branch": true,
		"add": true, "commit": true, "show": true, "stash": true,
	}
	if !allowed[command] {
		return Result{Error: fmt.Sprintf("git %q not allowed. Allowed: status, diff, log, branch, add, commit, show, stash", command)}
	}

	// Block dangerous git flags that can execute arbitrary commands.
	if containsDangerousGitFlag(extraArgs) {
		return Result{Error: "git -c/--config-env/--exec-path flags are not allowed"}
	}

	// For destructive operations, require extra confirmation
	if command == "commit" {
		if !hasCommitMessageFlag(extraArgs) {
			return Result{Error: "git commit requires -m flag with message"}
		}
	}

	cmdArgs := []string{"--no-pager", command}
	if extraArgs != "" {
		cmdArgs = append(cmdArgs, strings.Fields(extraArgs)...)
	}

	// Add safety flags
	switch command {
	case "log":
		cmdArgs = []string{"--no-pager", "log", "--oneline", "-20"}
		if extraArgs != "" {
			cmdArgs = append(cmdArgs, strings.Fields(extraArgs)...)
		}
	}

	cmd, err := sandboxCmd(ctx, t.workspaceRoot, "git", cmdArgs...)
	if err != nil {
		return Result{Error: fmt.Sprintf("sandbox unavailable: %v", err)}
	}
	cmd.Dir = t.workspaceRoot

	output, err := cmd.CombinedOutput()
	if err != nil {
		return Result{
			Output: string(output),
			Error:  fmt.Sprintf("git %s failed: %v", command, err),
		}
	}

	out := strings.TrimSpace(string(output))
	if out == "" {
		out = fmt.Sprintf("git %s: (no output)", command)
	}

	return Result{Success: true, Output: out}
}

// hasCommitMessageFlag checks whether extraArgs contains a -m flag with a message.
// Uses token-aware matching so --amend, --no-merge etc. don't false-positive.
func hasCommitMessageFlag(extraArgs string) bool {
	for _, token := range strings.Fields(extraArgs) {
		if token == "-m" {
			return true
		}
		if strings.HasPrefix(token, "-m") && len(token) > 2 && token[2] != '-' {
			return true // -m"msg" or -m=msg
		}
		// --message=<msg> is the long form
		if token == "--message" || strings.HasPrefix(token, "--message=") {
			return true
		}
	}
	return false
}

// containsDangerousGitFlag checks for git config injection vectors.
// git -c name=value, -c=name=value, and --config-env=NAME all run
// before the subcommand and can enable arbitrary command execution
// (e.g., -c core.pager='rm -rf /' log).
func containsDangerousGitFlag(extraArgs string) bool {
	fields := strings.Fields(extraArgs)
	for _, f := range fields {
		// -c name=value or -c=name=value
		if f == "-c" || strings.HasPrefix(f, "-c=") || strings.HasPrefix(f, "-c") {
			return true
		}
		// --config-env (git 2.44+)
		if f == "--config-env" || strings.HasPrefix(f, "--config-env=") {
			return true
		}
		// --exec-path
		if f == "--exec-path" || strings.HasPrefix(f, "--exec-path=") {
			return true
		}
		// --git-dir and --work-tree redirect operations to arbitrary repos
		if f == "--git-dir" || strings.HasPrefix(f, "--git-dir=") {
			return true
		}
		if f == "--work-tree" || strings.HasPrefix(f, "--work-tree=") {
			return true
		}
		// -C <path> changes working directory
		if f == "-C" {
			return true
		}
	}
	return false
}
