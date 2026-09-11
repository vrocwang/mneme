// Doctor extension for Mneme.
//
// Provides:
//   - doctor_run: workspace diagnostics (workspace, config, data directory)
//
// This was previously compiled into the core as internal/doctor. It is a
// self-contained, optional diagnostic, so it lives as an extension like the
// other non-core tools. The workspace is read from MNEME_HOME, which the host
// exports before launching extensions.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "doctor",
		Version:     "0.1.0",
		Description: "Workspace diagnostics: verify workspace, config, and data directories",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "doctor_run",
		Description: "Run workspace diagnostics: check the workspace, config file, and data directory. Returns a report with issues categorized as error, warning, or info.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Permission: "read_only",
		HasEffects: false,
	}, doctorRun)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "doctor: %v\n", err)
		os.Exit(1)
	}
}

type issue struct {
	Level   string `json:"level"` // error | warning | info
	Message string `json:"message"`
}

type report struct {
	WorkspacePath string  `json:"workspace_path"`
	Issues        []issue `json:"issues"`
}

func diagnose(workspacePath string) report {
	r := report{WorkspacePath: workspacePath}

	info, err := os.Stat(workspacePath)
	if err != nil || !info.IsDir() {
		r.Issues = append(r.Issues, issue{Level: "error", Message: "workspace directory does not exist"})
		return r
	}

	configPath := filepath.Join(workspacePath, "config", "config.toml")
	if _, err := os.Stat(configPath); err != nil {
		r.Issues = append(r.Issues, issue{Level: "warning", Message: "config file not found at " + configPath})
	}

	dataDir := filepath.Join(workspacePath, "data")
	if info, err := os.Stat(dataDir); err != nil || !info.IsDir() {
		r.Issues = append(r.Issues, issue{Level: "warning", Message: "data directory not found at " + dataDir})
	}

	return r
}

func doctorRun(_ context.Context, _ map[string]interface{}) extsdk.Result {
	r := diagnose(extsdk.WorkspaceDir())
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	return extsdk.Result{Success: true, Output: string(b)}
}
