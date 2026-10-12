package middleware

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/simon/mneme/internal/approval"
	"github.com/simon/mneme/internal/security"
	pkgtools "github.com/simon/mneme/pkg/tools"
)

// fakeInvokable is a minimal tool.InvokableTool used to observe whether the
// wrapper executes the inner tool and what context it receives.
type fakeInvokable struct {
	name   string
	run    func(ctx context.Context, args string) (string, error)
	gotCtx context.Context
}

func (f *fakeInvokable) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: f.name}, nil
}

func (f *fakeInvokable) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	f.gotCtx = ctx
	if f.run != nil {
		return f.run(ctx, args)
	}
	return "ok", nil
}

// infoOnlyTool implements only tool.BaseTool (no InvokableRun).
type infoOnlyTool struct{ name string }

func (t *infoOnlyTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}

func TestToolWrapper_NilControllers_Passthrough(t *testing.T) {
	inner := &fakeInvokable{name: "echo"}
	wrapped := NewToolWrapper(inner, nil, nil)

	out, err := wrapped.(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("nil controllers should not produce errors: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected inner output, got %q", out)
	}
}

func TestToolWrapper_SecurityDenies_BlocksExecution(t *testing.T) {
	gate := approval.NewGate(nil, nil, secTestLogger, true)
	sec := &SecurityMiddleware{ApprovalGate: gate}
	cb := NewCircuitBreaker()

	ran := false
	inner := &fakeInvokable{name: "shell", run: func(context.Context, string) (string, error) {
		ran = true
		return "should not reach", nil
	}}
	wrapped := NewToolWrapper(inner, sec, cb)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := wrapped.(tool.InvokableTool).InvokableRun(ctx, `{"command":"rm -rf /"}`)
	if err != nil {
		t.Fatalf("a refusal is a tool-level result, not a fatal error: %v", err)
	}
	if !strings.HasPrefix(out, pkgtools.ToolFailurePrefix) {
		t.Fatalf("refusal must reach the model as tool output, got %q", out)
	}
	if ran {
		t.Error("inner tool must not execute when approval is denied")
	}
	// A single denied call records one failure; three identical denials trip
	// the repeat-failure breaker, proving denials are counted as failures.
	for i := 0; i < 2; i++ {
		if _, e := wrapped.(tool.InvokableTool).InvokableRun(ctx, `{"command":"rm -rf /"}`); e != nil {
			t.Fatalf("repeated refusal should stay a tool result, got %v", e)
		}
	}
	if !cb.IsTripped() {
		t.Error("breaker should trip after repeated denied calls")
	}
}

// TestToolWrapper_ToolFailureFedBack is the contract that lets the model
// recover from a failed call: a failure the tool reports about its own work
// becomes visible tool output (prefixed so the stream layer can flag it) rather
// than an error that aborts the turn.
func TestToolWrapper_ToolFailureFedBack(t *testing.T) {
	cb := NewCircuitBreaker()
	inner := &fakeInvokable{name: "read_file", run: func(context.Context, string) (string, error) {
		return "", &pkgtools.ToolFailure{Message: "file not found: /nope"}
	}}
	wrapped := NewToolWrapper(inner, nil, cb)

	out, err := wrapped.(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("tool failure must not be fatal: %v", err)
	}
	want := pkgtools.ToolFailurePrefix + "file not found: /nope"
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
	if cb.IsTripped() {
		t.Error("one failure must not trip the breaker")
	}
}

// TestToolWrapper_ToolFailureKeepsPartialOutput covers a tool that produced
// output and then failed: the model must see both.
func TestToolWrapper_ToolFailureKeepsPartialOutput(t *testing.T) {
	inner := &fakeInvokable{name: "shell", run: func(context.Context, string) (string, error) {
		return "partial stdout", &pkgtools.ToolFailure{Message: "exit status 1"}
	}}
	wrapped := NewToolWrapper(inner, nil, nil)

	out, err := wrapped.(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The marker must lead, otherwise the runner cannot classify the result and a
	// failing call with output would be treated as a success.
	if !pkgtools.IsToolFailure(out) {
		t.Fatalf("output = %q, want it to start with the failure marker", out)
	}
	body := pkgtools.StripToolFailureMarker(out)
	if !strings.Contains(body, "partial stdout") || !strings.Contains(body, pkgtools.ToolFailureDisplay+"exit status 1") {
		t.Fatalf("output = %q, want the partial output and the failure text", body)
	}
	// A tool that legitimately prints the display text is NOT a failure.
	ok := &fakeInvokable{name: "echo", run: func(context.Context, string) (string, error) {
		return "Tool error: this is just text I printed", nil
	}}
	echoed, err := NewToolWrapper(ok, nil, nil).(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pkgtools.IsToolFailure(echoed) {
		t.Fatalf("legitimate output %q must not be classified as a failure", echoed)
	}
}

// TestToolWrapper_InfrastructureErrorStillFatal keeps the distinction: errors
// that are not tool-reported failures (transport, wiring, programming errors)
// must still abort rather than being laundered into model input.
func TestToolWrapper_InfrastructureErrorStillFatal(t *testing.T) {
	inner := &fakeInvokable{name: "broken", run: func(context.Context, string) (string, error) {
		return "", errors.New("transport reset")
	}}
	wrapped := NewToolWrapper(inner, nil, nil)

	if _, err := wrapped.(tool.InvokableTool).InvokableRun(context.Background(), `{}`); err == nil {
		t.Fatal("infrastructure errors must still propagate")
	}
}

func TestToolWrapper_ScrubsOutput(t *testing.T) {
	sec := &SecurityMiddleware{ApprovalGate: autoApproveGate()}
	inner := &fakeInvokable{name: "read", run: func(context.Context, string) (string, error) {
		return "key=sk-abcdefghijklmnopqrstuvwxyz1234567890", nil
	}}
	wrapped := NewToolWrapper(inner, sec, nil)

	out, err := wrapped.(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("output should be scrubbed, got: %s", out)
	}
}

func TestToolWrapper_RecordsFailureOnError(t *testing.T) {
	cb := NewCircuitBreaker()
	inner := &fakeInvokable{name: "flaky", run: func(context.Context, string) (string, error) {
		return "", errors.New("boom")
	}}
	wrapped := NewToolWrapper(inner, nil, cb)

	if _, err := wrapped.(tool.InvokableTool).InvokableRun(context.Background(), `{}`); err == nil {
		t.Fatal("expected inner error to propagate")
	}
	// Two more failures trip the repeat breaker (3 total).
	wrapped.(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	wrapped.(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	if !cb.IsTripped() {
		t.Error("breaker should trip after repeated tool errors")
	}
}

func TestToolWrapper_RecordsSuccess(t *testing.T) {
	cb := NewCircuitBreaker()
	inner := &fakeInvokable{name: "ok"}
	wrapped := NewToolWrapper(inner, nil, cb)

	if _, err := wrapped.(tool.InvokableTool).InvokableRun(context.Background(), `{}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cb.IsTripped() {
		t.Error("successful execution must not trip the breaker")
	}
}

func TestToolWrapper_NonInvokableReturnsError(t *testing.T) {
	wrapped := NewToolWrapper(&infoOnlyTool{name: "meta"}, nil, nil)
	_, err := wrapped.(tool.InvokableTool).InvokableRun(context.Background(), `{}`)
	if err == nil {
		t.Fatal("non-InvokableTool should produce a descriptive error")
	}
}

func TestToolWrapper_MarksApprovalHandled(t *testing.T) {
	// With an auto-approving gate the wrapper marks the context so that
	// defense-in-depth tools (e.g. shell tier gate) know approval ran.
	sec := &SecurityMiddleware{ApprovalGate: autoApproveGate()}
	var seenCtx context.Context
	inner := &fakeInvokable{name: "shell", run: func(ctx context.Context, _ string) (string, error) {
		seenCtx = ctx
		return "done", nil
	}}
	wrapped := NewToolWrapper(inner, sec, nil)

	if _, err := wrapped.(tool.InvokableTool).InvokableRun(context.Background(), `{}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seenCtx == nil || !security.IsApprovalHandled(seenCtx) {
		t.Error("wrapper should mark the context as approval-handled after CheckTool passes")
	}
}

func TestWrapAllTools_Idempotent(t *testing.T) {
	inner := &fakeInvokable{name: "echo"}
	once := NewToolWrapper(inner, nil, nil)
	twice := WrapAllTools([]tool.BaseTool{once}, nil, nil)
	if len(twice) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(twice))
	}
	if twice[0] != once {
		t.Error("already-wrapped tools should not be re-wrapped")
	}
}

func TestWrapAllTools_AllWrapped(t *testing.T) {
	tools := []tool.BaseTool{&fakeInvokable{name: "a"}, &fakeInvokable{name: "b"}}
	wrapped := WrapAllTools(tools, nil, nil)
	if len(wrapped) != 2 {
		t.Fatalf("expected 2 wrapped tools, got %d", len(wrapped))
	}
	for _, w := range wrapped {
		if _, ok := w.(*ToolWrapper); !ok {
			t.Error("every tool should be wrapped")
		}
	}
}
