package doctor

import (
	"context"
	"encoding/json"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/tools"
)

type doctorTool struct{ workspace string }

func (t *doctorTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        "doctor_run",
		Description: "Run workspace diagnostics: check config file existence and workspace health. Returns a report with issues categorized as error, warning, or info.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

func (t *doctorTool) Execute(ctx context.Context, args map[string]interface{}) tools.Result {
	report := Run(t.workspace)
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return tools.Result{Error: err.Error()}
	}
	return tools.Result{Success: true, Output: string(b)}
}

// RegisterTools registers doctor tools into the capability registry under the
// "doctor" set.
func RegisterTools(reg *capability.CapabilityRegistry, workspaceDir string) {
	reg.EnsureSet(&capability.CapabilitySet{
		ID:      "doctor",
		Name:    "Doctor",
		Kind:    capability.KindBuiltin,
		Enabled: true,
	})
	reg.RegisterTool("doctor", &doctorTool{workspace: workspaceDir})
}
