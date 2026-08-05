package tools

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/simon/mneme/internal/security"
)

// ── browser_open ─────────────────────────────────────────────────────

func NewBrowserOpen() Tool {
	return &browserOpenTool{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "browser_open",
				Description: "Opens a URL in the system's default web browser. Does not return page content — use the 'browser' tool for content retrieval.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{
							"type":        "string",
							"description": "URL to open in the browser.",
						},
					},
					"required": []string{"url"},
				},
			},
			PermLevel:      PermExecute,
			HasSideEffects: true,
			MaxOutputChars: 200,
			ToolCategory:   CategorySystem,
		},
	}
}

type browserOpenTool struct{ BaseTool }

func (t *browserOpenTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return Result{Error: "url is required"}
	}

	// SSRF check
	if err := validateURLFn(rawURL); err != nil {
		return Result{Error: fmt.Sprintf("url rejected: %v", err)}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", rawURL)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", rawURL)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Success: false, Error: fmt.Sprintf("open browser: %v", err), Output: string(out)}
	}
	return Result{Success: true, Output: fmt.Sprintf("Opened %s in browser.", rawURL)}
}

// ── image_info ───────────────────────────────────────────────────────

func NewImageInfo(workspaceRoot string) Tool {
	return &imageInfoTool{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "image_info",
				Description: "Returns dimensions, format, and file size of an image file in the workspace.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to the image file relative to workspace root.",
						},
					},
					"required": []string{"path"},
				},
			},
			PermLevel:      PermReadOnly,
			HasSideEffects: false,
			MaxOutputChars: 500,
			ToolCategory:   CategorySystem,
		},
		workspaceRoot: workspaceRoot,
	}
}

type imageInfoTool struct {
	BaseTool
	workspaceRoot string
}

func (t *imageInfoTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	relPath, _ := args["path"].(string)
	if relPath == "" {
		return Result{Error: "path is required"}
	}
	fullPath := filepath.Join(t.workspaceRoot, relPath)
	resolvedPath, err := security.ValidatePath(fullPath, t.workspaceRoot)
	if err != nil {
		return Result{Error: fmt.Sprintf("path blocked by security policy: %v", err)}
	}

	fi, err := os.Stat(resolvedPath)
	if err != nil {
		return Result{Error: fmt.Sprintf("stat: %v", err)}
	}

	f, err := os.Open(resolvedPath)
	if err != nil {
		return Result{Error: fmt.Sprintf("open: %v", err)}
	}
	defer f.Close()

	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return Result{Error: fmt.Sprintf("decode image: %v (not a supported image file?)", err)}
	}

	return Result{
		Success: true,
		Output: fmt.Sprintf("File: %s\nFormat: %s\nDimensions: %d × %d px\nSize: %d bytes (%.1f KB)",
			filepath.Base(relPath), format, cfg.Width, cfg.Height, fi.Size(), float64(fi.Size())/1024),
	}
}
