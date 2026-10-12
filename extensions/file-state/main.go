// File State extension for Mneme.
//
// Provides:
//   - file_snapshot:     take a baseline snapshot of a directory tree
//   - file_diff_state:   list files created/modified/deleted since the baseline
//
// This was previously compiled into the core as internal/file_state. It is an
// optional workspace-awareness helper, so it lives as an extension like the
// other non-core tools. The workspace is read from MNEME_HOME, which the host
// exports before launching extensions.
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
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
		Name:        "file-state",
		Version:     "0.1.0",
		Description: "Track file changes, checksums, and snapshots in the workspace",
	})

	// One tracker per extension process, matching the core's single
	// process-wide tracker registered at boot.
	tools := &fileTools{tracker: NewTracker()}

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "file_snapshot",
		Description: "Take a snapshot of current file state in a directory. Use before starting a task to track what files are created or modified.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Directory path to snapshot.",
				},
			},
			"required": []string{"path"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, tools.snapshot)

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "file_diff_state",
		Description: "Show files created, modified, or deleted since the last snapshot. Use after completing a task to report what changed.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Directory path to diff against last snapshot.",
				},
			},
			"required": []string{"path"},
		},
		Permission: "read_only",
		HasEffects: false,
	}, tools.diff)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "file-state: %v\n", err)
		os.Exit(1)
	}
}

// fileTools holds the process-wide tracker shared by both tools.
type fileTools struct{ tracker *Tracker }

// resolvePath turns a tool's `path` argument into an absolute path the same way
// the host's policy check does: a relative path is relative to the workspace
// root (extsdk.WorkspaceDir, i.e. MNEME_HOME), never to the extension process's
// CWD. Without this, a relative path can be authorised as <workspace>/x while
// the tracker reads <cwd>/x — the same argument naming a different file.
func resolvePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	root := extsdk.WorkspaceDir()
	if root == "" {
		return path
	}
	return filepath.Join(root, path)
}

// file_snapshot ─────────────────────────────────────────────────────────

func (t *fileTools) snapshot(_ context.Context, args map[string]interface{}) extsdk.Result {
	path := extsdk.GetStr(args, "path", "")
	if path == "" {
		return extsdk.Result{Error: "path is required"}
	}
	snap, err := t.tracker.TakeSnapshot(resolvePath(path))
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("snapshot: %v", err)}
	}
	b, err := json.Marshal(map[string]interface{}{
		"snapshot_id": snap.ID,
		"file_count":  len(snap.Files),
		"created_at":  snap.CreatedAt,
	})
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	return extsdk.Result{Success: true, Output: string(b)}
}

// file_diff_state ───────────────────────────────────────────────────────

func (t *fileTools) diff(_ context.Context, args map[string]interface{}) extsdk.Result {
	path := extsdk.GetStr(args, "path", "")
	if path == "" {
		return extsdk.Result{Error: "path is required"}
	}
	changes, err := t.tracker.Diff(resolvePath(path))
	if err != nil {
		return extsdk.Result{Error: fmt.Sprintf("diff: %v", err)}
	}
	if len(changes) == 0 {
		return extsdk.Result{Success: true, Output: "No file changes detected since last snapshot."}
	}
	b, err := json.MarshalIndent(changes, "", "  ")
	if err != nil {
		return extsdk.Result{Error: err.Error()}
	}
	return extsdk.Result{Success: true, Output: string(b)}
}
