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
	_, err := wrapped.(tool.InvokableTool).InvokableRun(ctx, `{"command":"rm -rf /"}`)
	if err == nil {
		t.Fatal("denied tool should return an error")
	}
	if ran {
		t.Error("inner tool must not execute when approval is denied")
	}
	// A single denied call records one failure; three identical denials trip
	// the repeat-failure breaker, proving denials are counted as failures.
	for i := 0; i < 2; i++ {
		if _, e := wrapped.(tool.InvokableTool).InvokableRun(ctx, `{"command":"rm -rf /"}`); e == nil {
			t.Fatal("denied tool should keep returning an error")
		}
	}
	if !cb.IsTripped() {
		t.Error("breaker should trip after repeated denied calls")
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
