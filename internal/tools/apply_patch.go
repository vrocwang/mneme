package tools

import (
	"context"
	"fmt"
	"os"
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

	// Validate every file path referenced by the diff itself. `patch -p1`
	// strips one leading component from each `---`/`+++` header, so a header
	// like `+++ a/../../.ssh/authorized_keys` (or an absolute path) would
	// escape the workspace. Resolve each referenced path against workDir and
	// reject any that land outside the workspace.
	if err := validatePatchPaths(patchContent, workDir, t.Workspace); err != nil {
		return Result{Error: err.Error()}
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
	cmd, err := sandboxCmd(ctx, t.Workspace, "patch", cmdArgs...)
	if err != nil {
		return Result{Error: fmt.Sprintf("sandbox unavailable: %v", err)}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Error: fmt.Sprintf("patch failed: %v — %s", err, strings.TrimSpace(string(out)))}
	}

	return Result{Success: true, Output: strings.TrimSpace(string(out))}
}

// validatePatchPaths extracts the file paths referenced by a unified diff and
// verifies each resolves within the workspace after applying the `patch -p1`
// prefix strip. It returns an error describing the first escaping path.
func validatePatchPaths(patch, workDir, workspace string) error {
	for _, line := range strings.Split(patch, "\n") {
		var p string
		switch {
		case strings.HasPrefix(line, "+++ "):
			p = strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
		case strings.HasPrefix(line, "--- "):
			p = strings.TrimSpace(strings.TrimPrefix(line, "--- "))
		default:
			continue
		}
		// Skip /dev/null (used for new-file and delete markers).
		if p == "" || p == "/dev/null" {
			continue
		}
		// Strip the trailing <TAB>timestamp that git appends to header paths.
		if i := strings.IndexByte(p, '\t'); i >= 0 {
			p = p[:i]
		}
		// Mimic `patch -p1`: drop the first path component.
		if i := strings.IndexByte(p, '/'); i >= 0 {
			p = p[i+1:]
		}
		if p == "" {
			continue
		}
		full := filepath.Clean(filepath.Join(workDir, p))
		if _, err := security.ValidatePath(full, workspace); err != nil {
			return fmt.Errorf("patch references a path outside the workspace: %s", p)
		}
	}
	return nil
}
