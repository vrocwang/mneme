package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/simon/mneme/internal/security"
)

const maxEditFileBytes = 5 * 1024 * 1024 // 5MB

// EditFile performs exact string replacement in a file.
type EditFile struct {
	workspaceRoot string
}

func NewEditFile(workspaceRoot string) *EditFile {
	return &EditFile{workspaceRoot: workspaceRoot}
}

func (t *EditFile) Schema() Schema {
	return Schema{
		Name:        "edit_file",
		Description: "Performs exact string replacement in a file. Finds old_string and replaces it with new_string. The old_string must match exactly once (or use replace_all for all occurrences).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "File path relative to workspace root.",
				},
				"old_string": map[string]interface{}{
					"type":        "string",
					"description": "Exact text to find and replace.",
				},
				"new_string": map[string]interface{}{
					"type":        "string",
					"description": "Replacement text.",
				},
				"replace_all": map[string]interface{}{
					"type":        "boolean",
					"description": "Replace all occurrences (default: false, requires single match).",
				},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
	}
}

func (t *EditFile) PermissionLevel() PermissionLevel { return PermWrite }
func (t *EditFile) SideEffects() bool                { return true }

func (t *EditFile) Execute(ctx context.Context, args map[string]interface{}) Result {
	relPath, ok := args["path"].(string)
	if !ok || relPath == "" {
		return Result{Success: false, Error: "path is required"}
	}
	oldStr, ok := args["old_string"].(string)
	if !ok || oldStr == "" {
		return Result{Success: false, Error: "old_string is required"}
	}
	newStr, ok := args["new_string"].(string)
	if !ok || newStr == "" {
		return Result{Success: false, Error: "new_string is required"}
	}
	replaceAll, _ := args["replace_all"].(bool)

	absPath := filepath.Join(t.workspaceRoot, relPath)

	// Security: use centralized path validation with symlink resolution.
	resolvedPath, err := security.ValidatePath(absPath, t.workspaceRoot)
	if err != nil {
		return Result{Success: false, Error: fmt.Sprintf("path rejected: %v", err)}
	}
	absPath = filepath.Clean(resolvedPath)

	info, err := os.Stat(absPath)
	if err != nil {
		return Result{Success: false, Error: fmt.Sprintf("cannot stat file: %v", err)}
	}
	if info.IsDir() {
		return Result{Success: false, Error: "path is a directory, not a file"}
	}
	if info.Size() > maxEditFileBytes {
		return Result{Success: false, Error: fmt.Sprintf("file too large (%d bytes, max %d)", info.Size(), maxEditFileBytes)}
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return Result{Success: false, Error: fmt.Sprintf("read file: %v", err)}
	}

	original := string(content)

	if !replaceAll {
		count := strings.Count(original, oldStr)
		if count == 0 {
			return Result{Success: false, Error: "old_string not found in file"}
		}
		if count > 1 {
			return Result{Success: false, Error: fmt.Sprintf("old_string found %d times — use replace_all or make the match more specific", count)}
		}
	}

	newContent := strings.ReplaceAll(original, oldStr, newStr)
	if newContent == original {
		return Result{Success: false, Error: "no changes made — old_string not found"}
	}

	// Atomic write via temp file + rename to avoid partial writes on crash.
	tmpPath := absPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(newContent), info.Mode()); err != nil {
		return Result{Success: false, Error: fmt.Sprintf("write temp file: %v", err)}
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		os.Remove(tmpPath)
		return Result{Success: false, Error: fmt.Sprintf("rename temp file: %v", err)}
	}

	replacements := strings.Count(original, oldStr)
	return Result{
		Success: true,
		Output:  fmt.Sprintf("File edited: %s (%d replacement(s))", relPath, replacements),
	}
}
