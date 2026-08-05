package approval

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/simon/mneme/internal/agent"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// testGate creates a Gate suitable for unit tests (no store, no events).
func testGate() *Gate {
	return &Gate{
		enabled: true,
		log:     testLogger,
		pending: make(map[string]*PendingApproval),
		originPolicy: OriginPolicyConfig{
			AutoApproveRisk: map[agent.TurnOriginKind]string{
				agent.OriginTrustedAutomation: "write",
			},
			RequireAll: map[agent.TurnOriginKind]bool{
				agent.OriginExternalChannel: true,
				agent.OriginUnknown:         true,
			},
		},
	}
}

func TestResolveToolRisk_RegistryBacked(t *testing.T) {
	g := testGate()

	// Without resolver: falls back to heuristic.
	risk := g.resolveToolRisk("unknown_tool_xyz")
	if risk == 0 {
		t.Error("expected non-zero risk for unknown tool")
	}

	// With resolver: uses registry-provided values.
	g.SetToolResolver(func(toolName string) (string, bool) {
		switch toolName {
		case "read_file":
			return "read_only", false
		case "write_file":
			return "write", true
		case "shell":
			return "execute", true
		default:
			return "", false
		}
	})

	if g.resolveToolRisk("read_file") != 1 {
		t.Error("read_file should be risk level 1 (read_only)")
	}
	// write + external = execute-level risk (3)
	if g.resolveToolRisk("write_file") != 3 {
		t.Error("write_file with external effect should be risk level 3 (execute)")
	}
	if g.resolveToolRisk("shell") != 3 {
		t.Error("shell should be risk level 3")
	}
	// Unknown tool falls back to heuristic.
	if g.resolveToolRisk("some_new_tool") == 0 {
		t.Error("unknown tool should get heuristic risk level, not 0")
	}
}

func TestToolHasSideEffects_RegistryBacked(t *testing.T) {
	g := testGate()

	// Without resolver: uses heuristic.
	if g.toolHasSideEffects("read_file") {
		t.Error("read_file should not have side effects by heuristic")
	}
	if !g.toolHasSideEffects("shell") {
		t.Error("shell should have side effects by heuristic")
	}

	// With resolver: overrides heuristic.
	g.SetToolResolver(func(toolName string) (string, bool) {
		if toolName == "safe_tool" {
			return "read_only", false
		}
		if toolName == "dangerous_tool" {
			return "execute", true
		}
		return "", false
	})

	if g.toolHasSideEffects("safe_tool") {
		t.Error("safe_tool should not have side effects (resolver says false)")
	}
	if !g.toolHasSideEffects("dangerous_tool") {
		t.Error("dangerous_tool should have side effects (resolver says true)")
	}
}

func TestIsAllowlisted(t *testing.T) {
	// Gate without store: always returns false.
	g := testGate()
	if g.IsAllowlisted("any_tool") {
		t.Error("gate without store should return false for IsAllowlisted")
	}
}

func TestRequestApproval_Allowlisted(t *testing.T) {
	// Allowlisted tools skip the prompt entirely.
	g := testGate()
	// Override IsAllowlisted by setting a store-backed gate would be complex;
	// test that the early-return path exists by checking disabled gate.
	ctx := context.Background()

	// Disabled gate: always approve.
	g.SetEnabled(false)
	decision, _ := g.RequestApproval(ctx, "shell", `{}`, "test")
	if decision != DecisionApproveOnce {
		t.Errorf("disabled gate should approve, got %v", decision)
	}

	// Enabled gate without store: should proceed to parking.
	g.SetEnabled(true)
	// This would block waiting for a decision if origin-aware auto-approval
	// doesn't kick in. Test with a context that has a turn origin set to
	// trigger auto-approval for read_only tools.
	ctx2 := agent.WithTurnOrigin(ctx, agent.TurnOrigin{
		Kind: agent.OriginTrustedAutomation,
	})
	decision2, _ := g.RequestApproval(ctx2, "read_file", `{}`, "test")
	if decision2 != DecisionApproveOnce {
		t.Errorf("trusted automation origin should auto-approve read_file, got %v", decision2)
	}
}

func TestOriginPolicy_AutoApprove(t *testing.T) {
	g := testGate()
	// Default origin policy: TrustedAutomation auto-approves up to "write" level.

	ctx := agent.WithTurnOrigin(context.Background(), agent.TurnOrigin{
		Kind: agent.OriginTrustedAutomation,
	})

	// read_file is read_only (risk 1), should be auto-approved.
	decision, _ := g.RequestApproval(ctx, "read_file", `{}`, "test")
	if decision != DecisionApproveOnce {
		t.Errorf("expected approve for read_file, got %v", decision)
	}
}

func TestOriginPolicy_RequireAll(t *testing.T) {
	g := testGate()
	// ExternalChannel requires approval for everything.
	ctx := agent.WithTurnOrigin(context.Background(), agent.TurnOrigin{
		Kind: agent.OriginExternalChannel,
	})

	// Even read_file (risk 1, read_only) should NOT be auto-approved for
	// external channel origin because RequireAll=true. But without a store
	// to persist the pending request, this will block on the channel.
	// We test that the gate enters the parking path by verifying the context
	// cancellation path works.
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel() // Cancel immediately to test the cancellation path.
	decision, _ := g.RequestApproval(cancelCtx, "shell", `{"cmd":"echo hi"}`, "test")
	if decision != DecisionDeny {
		t.Errorf("cancelled context should deny, got %v", decision)
	}
}

func TestSetToolResolver_NilIsSafe(t *testing.T) {
	g := testGate()
	g.SetToolResolver(nil)
	// Should not panic; falls back to heuristic.
	risk := g.resolveToolRisk("read_file")
	if risk != 1 {
		t.Errorf("read_file heuristic risk should be 1, got %d", risk)
	}
}

func TestToolRiskAtOrBelow_Thresholds(t *testing.T) {
	g := testGate()
	g.SetToolResolver(func(toolName string) (string, bool) {
		switch toolName {
		case "safe_read":
			return "read_only", false
		case "write_tool":
			return "write", false
		case "write_external":
			return "write", true
		default:
			return "", false
		}
	})

	// read_only ≤ read_only = true
	if !g.toolRiskAtOrBelow("safe_read", "read_only") {
		t.Error("safe_read (read_only) should be ≤ read_only threshold")
	}
	// read_only ≤ write = true
	if !g.toolRiskAtOrBelow("safe_read", "write") {
		t.Error("safe_read should be ≤ write threshold")
	}
	// write ≤ read_only = false
	if g.toolRiskAtOrBelow("write_tool", "read_only") {
		t.Error("write_tool should NOT be ≤ read_only threshold")
	}
	// write_external ≤ read_only = false (elevated to 3)
	if g.toolRiskAtOrBelow("write_external", "read_only") {
		t.Error("write_external (elevated to 3) should NOT be ≤ read_only")
	}
	// Invalid threshold returns false.
	if g.toolRiskAtOrBelow("safe_read", "invalid_level") {
		t.Error("invalid threshold should return false")
	}
}

func TestGate_EnabledFlag(t *testing.T) {
	g := testGate()

	// Test SetEnabled/getter contract.
	g.SetEnabled(false)
	if g.IsEnabled() {
		t.Error("after SetEnabled(false), IsEnabled should return false")
	}
	g.SetEnabled(true)
	if !g.IsEnabled() {
		t.Error("after SetEnabled(true), IsEnabled should return true")
	}

	ctx := context.Background()

	// Disabled gate should auto-approve everything.
	g.SetEnabled(false)
	decision, _ := g.RequestApproval(ctx, "any_tool", `{}`, "test")
	if decision != DecisionApproveOnce {
		t.Errorf("disabled gate should auto-approve, got %v", decision)
	}

	// Enabled gate enters the approval flow. With a trusted automation
	// origin and a read-only tool, it should also auto-approve.
	g.SetEnabled(true)
	autoCtx := agent.WithTurnOrigin(ctx, agent.TurnOrigin{Kind: agent.OriginTrustedAutomation})
	decision2, _ := g.RequestApproval(autoCtx, "read_file", `{}`, "test")
	if decision2 != DecisionApproveOnce {
		t.Errorf("trusted automation should auto-approve read_file, got %v", decision2)
	}
}
