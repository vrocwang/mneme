package eino

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/prompts"
)

// fakeBaseTool is a minimal tool.BaseTool used to exercise allowlist filtering.
type fakeBaseTool struct{ name string }

func (f *fakeBaseTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: f.name}, nil
}

func toolsOf(names ...string) []tool.BaseTool {
	out := make([]tool.BaseTool, 0, len(names))
	for _, n := range names {
		out = append(out, &fakeBaseTool{name: n})
	}
	return out
}

func TestFilterToolsByAllowlist_EmptyAllowlistReturnsNil(t *testing.T) {
	if got := filterAgentTools(toolsOf("a", "b"), nil, nil); got != nil {
		t.Errorf("empty allowlist should return nil, got %d tools", len(got))
	}
}

func TestFilterToolsByAllowlist_EmptyInputReturnsNil(t *testing.T) {
	if got := filterAgentTools(nil, []string{"a"}, nil); got != nil {
		t.Errorf("empty input should return nil, got %d tools", len(got))
	}
}

func TestFilterToolsByAllowlist_WildcardReturnsAll(t *testing.T) {
	in := toolsOf("a", "b", "c")
	got := filterAgentTools(in, []string{"*"}, nil)
	if len(got) != 3 {
		t.Fatalf("wildcard should return all tools, got %d", len(got))
	}
	// Wildcard must return a copy, not the original slice.
	got[0] = nil
	if in[0] == nil {
		t.Error("wildcard must copy the input slice, not alias it")
	}
}

func TestFilterToolsByAllowlist_SpecificNames(t *testing.T) {
	got := filterAgentTools(toolsOf("read_file", "write_file", "shell"), []string{"read_file", "shell"}, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(got))
	}
	seen := map[string]bool{}
	for _, t := range got {
		info, _ := t.Info(context.Background())
		seen[info.Name] = true
	}
	if !seen["read_file"] || !seen["shell"] {
		t.Errorf("expected read_file and shell, got %v", seen)
	}
}

func TestFilterToolsByAllowlist_NoMatch(t *testing.T) {
	got := filterAgentTools(toolsOf("a", "b"), []string{"missing"}, nil)
	if len(got) != 0 {
		t.Errorf("expected 0 tools for non-matching allowlist, got %d", len(got))
	}
}

func TestFilterToolsByAllowlist_PreservesOrder(t *testing.T) {
	got := filterAgentTools(toolsOf("z", "a", "m"), []string{"a", "m", "z"}, nil)
	if len(got) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(got))
	}
	// Output order follows the input order, not the allowlist order.
	names := make([]string, 0, len(got))
	for _, t := range got {
		info, _ := t.Info(context.Background())
		names = append(names, info.Name)
	}
	if names[0] != "z" || names[1] != "a" || names[2] != "m" {
		t.Errorf("expected input order z,a,m, got %v", names)
	}
}

func TestFilterAgentTools_DenylistWins(t *testing.T) {
	// A wildcard allowlist admits everything except the denylist entries.
	got := filterAgentTools(toolsOf("a", "b", "c"), []string{"*"}, []string{"b"})
	if len(got) != 2 {
		t.Fatalf("expected 2 tools after denylist, got %d", len(got))
	}
	for _, tl := range got {
		info, _ := tl.Info(context.Background())
		if info.Name == "b" {
			t.Error("denied tool b must not survive the filter")
		}
	}
	// The denylist also subtracts from an explicit allowlist.
	got = filterAgentTools(toolsOf("read_file", "shell"), []string{"read_file", "shell"}, []string{"shell"})
	if len(got) != 1 {
		t.Fatalf("expected 1 tool after denylist, got %d", len(got))
	}
	info, _ := got[0].Info(context.Background())
	if info.Name != "read_file" {
		t.Errorf("expected read_file to survive, got %q", info.Name)
	}
}

func TestBuildInstruction_NilManager(t *testing.T) {
	if got := buildInstruction(nil, prompts.NameFromAgentID("general")); got != "" {
		t.Errorf("nil manager should return empty string, got %q", got)
	}
}

func TestBuildInstruction_LoadsEmbeddedDefault(t *testing.T) {
	mgr := prompts.New("")
	got := buildInstruction(mgr, prompts.NameFromAgentID("general"))
	if got == "" {
		t.Error("embedded agent_general prompt should resolve to non-empty text")
	}
}

// fakeChatModel is a minimal model.ToolCallingChatModel for failover tests.
type fakeChatModel struct{ tag string }

func (f fakeChatModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return &schema.Message{Content: f.tag}, nil
}
func (fakeChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}
func (f fakeChatModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func TestBuildFailoverConfig_Empty(t *testing.T) {
	// With no backup models, failover is disabled entirely (nil config).
	if cfg := buildFailoverConfig(nil); cfg != nil {
		t.Error("expected nil config when no failover models are configured")
	}
}

func TestBuildFailoverConfig_WithModels(t *testing.T) {
	cfg := buildFailoverConfig([]model.ToolCallingChatModel{
		fakeChatModel{tag: "primary"},
		fakeChatModel{tag: "backup"},
	})
	ctx := context.Background()

	m1, _, err := cfg.GetFailoverModel(ctx, nil)
	if err != nil {
		t.Fatalf("first failover should succeed: %v", err)
	}
	if m1 == nil {
		t.Fatal("first failover model should not be nil")
	}
	m2, _, err := cfg.GetFailoverModel(ctx, nil)
	if err != nil {
		t.Fatalf("second failover should succeed: %v", err)
	}
	if m2 == nil {
		t.Fatal("second failover model should not be nil")
	}
	if _, _, err := cfg.GetFailoverModel(ctx, nil); err == nil {
		t.Error("third failover should exhaust and return an error")
	}
}

// guard against accidentally dropping the adk-import in future edits.
var _ adk.Agent

func TestCollectRouteModels_EmptyConfig(t *testing.T) {
	if m := CollectRouteModels(context.Background(), &config.Config{}, nil); m != nil {
		t.Errorf("expected nil map with no routes, got %d entries", len(m))
	}
	if m := CollectRouteModels(context.Background(), nil, nil); m != nil {
		t.Errorf("expected nil map for nil config, got %d entries", len(m))
	}
}

func TestCollectRouteModels_ResolvesConfiguredRoutes(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{{
			Name:   "openai",
			Type:   "openai",
			APIKey: "test-key",
			Models: []string{"model-a", "model-b"},
		}},
	}
	cfg.Agent.ModelRoutes = map[string]string{
		"coding":    "model-a",
		"reasoning": "model-b",
		"unset":     "", // empty route must be skipped, not error
	}
	got := CollectRouteModels(context.Background(), cfg, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 resolved routes, got %d", len(got))
	}
	for _, kind := range []string{"coding", "reasoning"} {
		if got[kind] == nil {
			t.Errorf("route %q was not resolved", kind)
		}
	}
	if _, ok := got["unset"]; ok {
		t.Error("empty route value should be skipped")
	}
}

func TestFinishContent_MaxIterationsNotice(t *testing.T) {
	// Hitting the iteration bound with no output yields a user-visible notice.
	if got := finishContent("", true); got != maxIterationsNotice {
		t.Errorf("empty partial result should yield the notice, got %q", got)
	}
	if got := finishContent("   ", true); got != maxIterationsNotice {
		t.Errorf("whitespace-only partial result should yield the notice, got %q", got)
	}
	// Partial content is returned unchanged.
	if got := finishContent("partial answer", true); got != "partial answer" {
		t.Errorf("partial content must be preserved, got %q", got)
	}
	// A normal completion is never modified.
	if got := finishContent("", false); got != "" {
		t.Errorf("normal completion should not inject a notice, got %q", got)
	}
}

func TestIsMaxIterationsErr(t *testing.T) {
	if isMaxIterationsErr(nil) {
		t.Error("nil error must not match the iteration limit")
	}
	if !isMaxIterationsErr(adk.ErrExceedMaxIterations) {
		t.Error("the adk sentinel must match")
	}
	wrapped := fmt.Errorf("agent failed: %w", adk.ErrExceedMaxIterations)
	if !isMaxIterationsErr(wrapped) {
		t.Error("a wrapped sentinel must match")
	}
	if !isMaxIterationsErr(errors.New("exceeds max iterations")) {
		t.Error("the message form must match")
	}
	if isMaxIterationsErr(errors.New("network timeout")) {
		t.Error("unrelated errors must not match")
	}
}
