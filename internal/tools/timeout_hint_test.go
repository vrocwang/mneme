package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// deadlineTool records the deadline its Execute received, optionally declaring
// a per-tool budget.
type deadlineTool struct {
	name  string
	hint  time.Duration
	got   time.Duration
	delay time.Duration
}

func (d *deadlineTool) Schema() Schema { return Schema{Name: d.name} }

func (d *deadlineTool) Execute(ctx context.Context, _ map[string]interface{}) Result {
	if dl, ok := ctx.Deadline(); ok {
		d.got = time.Until(dl)
	}
	if d.delay > 0 {
		select {
		case <-time.After(d.delay):
		case <-ctx.Done():
			return Result{Error: ctx.Err().Error()}
		}
	}
	return Result{Success: true, Output: "ok"}
}

func (d *deadlineTool) TimeoutHint() time.Duration { return d.hint }

// withGlobalTimeout pins the global tool timeout for one test and restores it.
func withGlobalTimeout(t *testing.T, secs int) {
	t.Helper()
	prev := ToolTimeoutSecs()
	envActive := envOverrideActive.Load()
	envOverrideActive.Store(false) // let the test push a value regardless of env
	SetToolTimeoutSecs(secs)
	t.Cleanup(func() {
		envOverrideActive.Store(envActive)
		globalTimeoutSecs.Store(int64(prev))
	})
}

// A declared per-tool budget must beat the global timeout, otherwise a tool
// that legitimately runs longer (a language server, a workflow await) is cut
// short and reports a bogus timeout to the model.
func TestExecuteWithTimeoutHonoursToolHint(t *testing.T) {
	withGlobalTimeout(t, 1)

	hinted := &deadlineTool{name: "slow", hint: 30 * time.Second}
	if res := ExecuteWithTimeout(context.Background(), hinted, nil, nil); res.Error != "" {
		t.Fatalf("hinted tool failed: %s", res.Error)
	}
	if hinted.got < 25*time.Second || hinted.got > 35*time.Second {
		t.Fatalf("deadline = %s, want the declared ~30s hint (global was 1s)", hinted.got)
	}

	plain := &deadlineTool{name: "plain"}
	ExecuteWithTimeout(context.Background(), plain, nil, nil)
	if plain.got > 2*time.Second {
		t.Fatalf("deadline = %s, want the 1s global timeout for a tool with no hint", plain.got)
	}
}

// An explicit per-call override outranks both the hint and the global timeout.
func TestExecuteWithTimeoutOverrideBeatsHint(t *testing.T) {
	withGlobalTimeout(t, 1)

	tool := &deadlineTool{name: "slow", hint: 30 * time.Second}
	ExecuteWithTimeout(context.Background(), tool, nil, &ToolCallOptions{TimeoutOverride: 2})
	if tool.got > 3*time.Second {
		t.Fatalf("deadline = %s, want the 2s per-call override to win over the hint", tool.got)
	}
}

// A hint is a request, not an escape hatch: it is clamped to the configured
// bounds so an extension cannot pin a call open indefinitely.
func TestExecuteWithTimeoutClampsHint(t *testing.T) {
	withGlobalTimeout(t, 5)

	tool := &deadlineTool{name: "greedy", hint: time.Duration(MaxTimeoutSecs+600) * time.Second}
	ExecuteWithTimeout(context.Background(), tool, nil, nil)
	if tool.got > time.Duration(MaxTimeoutSecs)*time.Second+time.Minute {
		t.Fatalf("deadline = %s, want it clamped to MaxTimeoutSecs=%ds", tool.got, MaxTimeoutSecs)
	}

	// A zero/negative hint means "no opinion" and must fall back to the global.
	neutral := &deadlineTool{name: "neutral", hint: 0}
	ExecuteWithTimeout(context.Background(), neutral, nil, nil)
	if neutral.got > 6*time.Second {
		t.Fatalf("deadline = %s, want the 5s global timeout when the hint is zero", neutral.got)
	}
}

// The hint is enforced, not just recorded: a tool that outlives its own budget
// is cancelled and reports the timeout to the caller.
func TestExecuteWithTimeoutEnforcesHintDeadline(t *testing.T) {
	withGlobalTimeout(t, 30)

	tool := &deadlineTool{name: "overrun", hint: time.Second, delay: 10 * time.Second}
	start := time.Now()
	res := ExecuteWithTimeout(context.Background(), tool, nil, nil)
	elapsed := time.Since(start)
	if res.Error == "" {
		t.Fatal("a tool that overruns its hinted budget must fail")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("waited %s; the 1s hint (not the 30s global) must bound the call", elapsed)
	}
}

// The extension wire carries the declaration; the host must surface it as a
// TimeoutHintTool and leave undeclared tools on the global timeout.
func TestProtoToolTimeoutHintFollowsDeclaration(t *testing.T) {
	var declared protoToolDef
	if err := json.Unmarshal([]byte(`{"name":"await_workflow","timeout_secs":300}`), &declared); err != nil {
		t.Fatal(err)
	}
	pt := &protoTool{def: declared}
	if got := pt.TimeoutHint(); got != 300*time.Second {
		t.Fatalf("TimeoutHint() = %s, want 300s", got)
	}
	var hinted TimeoutHintTool = pt // compile-time + runtime interface check
	if hinted.TimeoutHint() != 300*time.Second {
		t.Fatal("protoTool must satisfy TimeoutHintTool")
	}

	var undeclared protoToolDef
	if err := json.Unmarshal([]byte(`{"name":"read_file"}`), &undeclared); err != nil {
		t.Fatal(err)
	}
	if got := (&protoTool{def: undeclared}).TimeoutHint(); got != 0 {
		t.Fatalf("TimeoutHint() = %s, want 0 (no declaration → global timeout)", got)
	}
}
