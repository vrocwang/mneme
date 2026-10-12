package boot

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/simon/mneme/internal/approval"
	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/tools"
)

// declaredTool reports an explicit permission and side-effect flag, like the
// migrated extension tools do over the protocol.
type declaredTool struct {
	name     string
	perm     tools.PermissionLevel
	external bool
}

func (d *declaredTool) Schema() tools.Schema { return tools.Schema{Name: d.name} }
func (d *declaredTool) Execute(context.Context, map[string]interface{}) tools.Result {
	return tools.Result{Success: true}
}
func (d *declaredTool) PermissionLevel() tools.PermissionLevel { return d.perm }
func (d *declaredTool) SideEffects() bool                      { return d.external }

// A tool that declares nothing must keep falling back to the heuristics.
type undeclaredTool struct{ name string }

func (u *undeclaredTool) Schema() tools.Schema { return tools.Schema{Name: u.name} }
func (u *undeclaredTool) Execute(context.Context, map[string]interface{}) tools.Result {
	return tools.Result{Success: true}
}

// The gate is only useful if a tool's declared risk actually reaches it. This
// test fails if the resolver wiring is dropped, which would silently revert
// every migrated tool to name heuristics.
func TestApprovalGateConsultsDeclaredToolPermissions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	core := NewAppCore(config.DefaultConfig(), logger)
	core.ApprovalGate = approval.NewGate(nil, nil, logger, true)
	core.CapReg = capability.NewCapabilityRegistry()

	core.CapReg.RegisterTool("test", &declaredTool{name: "read_only_probe", perm: tools.PermReadOnly})
	core.CapReg.RegisterTool("test", &declaredTool{name: "write_probe", perm: tools.PermWrite, external: true})
	core.CapReg.RegisterTool("test", &undeclaredTool{name: "todo_add"})

	core.wireApprovalToolResolver()

	// Levels: 0 none, 1 read_only, 2 write, 3 execute.
	if got := core.ApprovalGate.ToolRisk("read_only_probe"); got != 1 {
		t.Errorf("declared read_only tool risk = %d, want 1 (the resolver must be installed)", got)
	}
	if got := core.ApprovalGate.ToolRisk("write_probe"); got != 3 {
		t.Errorf("declared write+external tool risk = %d, want 3 (write with external effects escalates)", got)
	}
	// An undeclared tool still resolves through the heuristics.
	if got := core.ApprovalGate.ToolRisk("todo_add"); got != 2 {
		t.Errorf("undeclared todo_add risk = %d, want 2 (heuristic fallback)", got)
	}
}
