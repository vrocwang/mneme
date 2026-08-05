package routing

import (
	"testing"
)

func TestNewRouter(t *testing.T) {
	r := NewRouter("gpt-4o")
	if r.fallback != "gpt-4o" {
		t.Errorf("expected fallback 'gpt-4o', got %q", r.fallback)
	}
}

func TestResolveFallback(t *testing.T) {
	r := NewRouter("llama3")
	model := r.Resolve(RouteDefault)
	if model != "llama3" {
		t.Errorf("expected fallback 'llama3', got %q", model)
	}
}

func TestSetAndResolveRoute(t *testing.T) {
	r := NewRouter("default-model")
	r.SetRoute(RouteCoding, "deepseek-coder")
	r.SetRoute(RouteReasoning, "claude-opus")

	if got := r.Resolve(RouteCoding); got != "deepseek-coder" {
		t.Errorf("expected 'deepseek-coder', got %q", got)
	}
	if got := r.Resolve(RouteReasoning); got != "claude-opus" {
		t.Errorf("expected 'claude-opus', got %q", got)
	}
	// Unknown kind falls back.
	if got := r.Resolve(RouteVision); got != "default-model" {
		t.Errorf("expected fallback 'default-model', got %q", got)
	}
}

func TestParseRouteKind(t *testing.T) {
	tests := []struct {
		input    string
		expected RouteKind
	}{
		{"coding", RouteCoding},
		{"CODING", RouteCoding},
		{"reasoning", RouteReasoning},
		{"summary", RouteSummary},
		{"summarization", RouteSummary},
		{"vision", RouteVision},
		{"unknown", RouteDefault},
		{"", RouteDefault},
	}
	for _, tc := range tests {
		got := ParseRouteKind(tc.input)
		if got != tc.expected {
			t.Errorf("ParseRouteKind(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
