package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/security"
)

// ApplyPatch applies a unified diff patch to files in the workspace.
type ApplyPatch struct {
	BaseTool
	Workspace string
}

func NewApplyPatch(workspace string) *ApplyPatch {
	return &ApplyPatch{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "apply_patch",
				Description: "Apply a unified diff patch to files in the workspace. Accepts a diff string and target file path.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"patch": map[string]interface{}{
							"type":        "string",
							"description": "Unified diff patch content",
						},
						"target": map[string]interface{}{
							"type":        "string",
							"description": "Target file to patch (relative to workspace)",
						},
					},
					"required": []string{"patch"},
				},
			},
			PermLevel:      PermWrite,
			HasSideEffects: true,
		},
		Workspace: workspace,
	}
}

func (t *ApplyPatch) Schema() Schema { return t.BaseTool.SchemaVal }

func (t *ApplyPatch) Execute(ctx context.Context, args map[string]interface{}) Result {
	if err := ctx.Err(); err != nil {
		return Result{Error: err.Error()}
	}

	patchContent, _ := args["patch"].(string)
	if patchContent == "" {
		return Result{Error: "patch content is required"}
	}
	target, _ := args["target"].(string)

	// Validate target is within workspace using security.ValidatePath.
	workDir := t.Workspace
	if target != "" {
		fullPath := filepath.Join(t.Workspace, target)
		resolvedPath, err := security.ValidatePath(fullPath, t.Workspace)
		if err != nil {
			return Result{Error: fmt.Sprintf("path rejected: %v", err)}
		}
		workDir = filepath.Dir(filepath.Clean(resolvedPath))
	}

	tmpFile, err := os.CreateTemp(config.TempDir(), "patch-*.diff")
	if err != nil {
		return Result{Error: fmt.Sprintf("create temp file: %v", err)}
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(patchContent); err != nil {
		tmpFile.Close()
		return Result{Error: fmt.Sprintf("write patch: %v", err)}
	}
	tmpFile.Close()

	cmdArgs := []string{"-p1", "-i", tmpPath, "-d", workDir}
	cmd := exec.CommandContext(ctx, "patch", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Error: fmt.Sprintf("patch failed: %v — %s", err, strings.TrimSpace(string(out)))}
	}

	return Result{Success: true, Output: strings.TrimSpace(string(out))}
}
