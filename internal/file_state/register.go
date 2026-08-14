package file_state

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/tools"
)

type snapshotTool struct{ tracker *Tracker }

func (t *snapshotTool) Schema() tools.Schema {
	return tools.Schema{
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
	}
}

func (t *snapshotTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	path, _ := args["path"].(string)
	if path == "" {
		return tools.Result{Error: "path is required"}
	}
	snap, err := t.tracker.TakeSnapshot(path)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("snapshot: %v", err)}
	}
	b, err := json.Marshal(map[string]interface{}{
		"snapshot_id": snap.ID,
		"file_count":  len(snap.Files),
		"created_at":  snap.CreatedAt,
	})
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	return tools.Result{Success: true, Output: string(b)}
}

type diffTool struct{ tracker *Tracker }

func (t *diffTool) Schema() tools.Schema {
	return tools.Schema{
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
	}
}

func (t *diffTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	path, _ := args["path"].(string)
	if path == "" {
		return tools.Result{Error: "path is required"}
	}
	changes, err := t.tracker.Diff(path)
	if err != nil {
		return tools.Result{Error: fmt.Sprintf("diff: %v", err)}
	}
	if len(changes) == 0 {
		return tools.Result{Success: true, Output: "No file changes detected since last snapshot."}
	}
	b, err := json.MarshalIndent(changes, "", "  ")
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// RegisterTools registers file_state tools into the capability registry under
// the "file-state" set.
func RegisterTools(reg *capability.CapabilityRegistry) {
	tracker := NewTracker()
	reg.EnsureSet(&capability.CapabilitySet{
		ID:      "file-state",
		Name:    "File State",
		Kind:    capability.KindBuiltin,
		Enabled: true,
	})
	reg.RegisterTool("file-state", &snapshotTool{tracker: tracker})
	reg.RegisterTool("file-state", &diffTool{tracker: tracker})
}
