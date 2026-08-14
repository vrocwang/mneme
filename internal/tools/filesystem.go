package tools

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	mnfs "github.com/simon/mneme/internal/fs"
	"github.com/simon/mneme/internal/security"
)

// ReadFile reads a file from disk via the filesystem seam.
type ReadFile struct {
	workspaceRoot string
	fs            mnfs.FS
}

func NewReadFile(workspaceRoot string) *ReadFile {
	return NewReadFileWithFS(workspaceRoot, mnfs.OS{})
}

// NewReadFileWithFS constructs ReadFile with an explicit filesystem provider.
// The default constructor uses the in-process OS provider; callers that need a
// process-isolated provider use this variant.
func NewReadFileWithFS(workspaceRoot string, provider mnfs.FS) *ReadFile {
	return &ReadFile{workspaceRoot: workspaceRoot, fs: provider}
}

func (t *ReadFile) Schema() Schema {
	return Schema{
		Name:        "read_file",
		Description: "Read the contents of a file",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to read",
				},
			},
			"required": []string{"path"},
		},
	}
}

const maxReadFileBytes = 5 * 1024 * 1024 // 5 MB

func (t *ReadFile) PermissionLevel() PermissionLevel { return PermReadOnly }
func (t *ReadFile) SideEffects() bool                { return false }

func (t *ReadFile) Execute(ctx context.Context, args map[string]interface{}) Result {
	path, ok := args["path"].(string)
	if !ok {
		return Result{Error: "path is required and must be a string"}
	}

	fullPath := filepath.Join(t.workspaceRoot, path)
	resolvedPath, err := security.ValidatePath(fullPath, t.workspaceRoot)
	if err != nil {
		return Result{Error: fmt.Sprintf("path blocked by security policy: %v", err)}
	}

	f, err := t.fs.Open(resolvedPath)
	if err != nil {
		return Result{Error: fmt.Sprintf("open file: %v", err)}
	}
	defer f.Close()

	// LimitReader enforces the size cap at read time, avoiding TOCTOU between
	// Stat and ReadFile where the file could grow (or be replaced) after the
	// size check.
	data, err := io.ReadAll(io.LimitReader(f, maxReadFileBytes))
	if err != nil {
		return Result{Error: fmt.Sprintf("read file: %v", err)}
	}
	return Result{Success: true, Output: string(data)}
}

// WriteFile writes content to a file via the filesystem seam.
type WriteFile struct {
	workspaceRoot string
	fs            mnfs.FS
}

func NewWriteFile(workspaceRoot string) *WriteFile {
	return NewWriteFileWithFS(workspaceRoot, mnfs.OS{})
}

func NewWriteFileWithFS(workspaceRoot string, provider mnfs.FS) *WriteFile {
	return &WriteFile{workspaceRoot: workspaceRoot, fs: provider}
}

func (t *WriteFile) Schema() Schema {
	return Schema{
		Name:        "write_file",
		Description: "Write content to a file, creating it if needed",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to write",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content to write",
				},
			},
			"required": []string{"path", "content"},
		},
	}
}

func (t *WriteFile) PermissionLevel() PermissionLevel { return PermWrite }
func (t *WriteFile) SideEffects() bool                { return true }

func (t *WriteFile) Execute(ctx context.Context, args map[string]interface{}) Result {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return Result{Error: "path is required and must be a string"}
	}
	content, ok := args["content"].(string)
	if !ok {
		return Result{Error: "content is required and must be a string"}
	}

	fullPath := filepath.Join(t.workspaceRoot, path)
	resolvedPath, err := security.ValidatePath(fullPath, t.workspaceRoot)
	if err != nil {
		return Result{Error: fmt.Sprintf("path blocked by security policy: %v", err)}
	}
	dir := filepath.Dir(resolvedPath)
	if err := t.fs.MkdirAll(dir, 0755); err != nil {
		return Result{Error: fmt.Sprintf("create dir: %v", err)}
	}
	if err := t.fs.WriteFile(resolvedPath, []byte(content), 0644); err != nil {
		return Result{Error: fmt.Sprintf("write file: %v", err)}
	}
	return Result{Success: true, Output: fmt.Sprintf("Wrote %d bytes to %s", len(content), path)}
}

// ListDir lists files in a directory via the filesystem seam.
type ListDir struct {
	workspaceRoot string
	fs            mnfs.FS
}

func NewListDir(workspaceRoot string) *ListDir {
	return NewListDirWithFS(workspaceRoot, mnfs.OS{})
}

func NewListDirWithFS(workspaceRoot string, provider mnfs.FS) *ListDir {
	return &ListDir{workspaceRoot: workspaceRoot, fs: provider}
}

func (t *ListDir) Schema() Schema {
	return Schema{
		Name:        "list_dir",
		Description: "List files and directories in a path",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to list (defaults to workspace root)",
				},
			},
		},
	}
}

func (t *ListDir) PermissionLevel() PermissionLevel { return PermReadOnly }
func (t *ListDir) SideEffects() bool                { return false }

func (t *ListDir) Execute(ctx context.Context, args map[string]interface{}) Result {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}
	fullPath := filepath.Join(t.workspaceRoot, path)
	resolvedPath, err := security.ValidatePath(fullPath, t.workspaceRoot)
	if err != nil {
		return Result{Error: fmt.Sprintf("path blocked by security policy: %v", err)}
	}

	entries, err := t.fs.ReadDir(resolvedPath)
	if err != nil {
		return Result{Error: fmt.Sprintf("list dir: %v", err)}
	}

	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			b.WriteString(e.Name() + "/\n")
		} else {
			b.WriteString(e.Name() + "\n")
		}
	}
	return Result{Success: true, Output: b.String()}
}
