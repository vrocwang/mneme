package skills

import "testing"

func TestRegistry_RegisterAndList(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{
		Name:        "web-search",
		Description: "Search the web",
		Version:     "1.0.0",
		ToolDescriptors: []ToolDescriptor{
			{Name: "search", Description: "Search query", Parameters: map[string]interface{}{"type": "object"}},
		},
	})

	list := r.List()
	if len(list) != 1 {
		t.Errorf("expected 1 skill, got %d", len(list))
	}

	got := r.Get("web-search")
	if got == nil {
		t.Error("expected to find web-search")
	}
}

func TestRegistry_AllToolDescriptors(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{
		Name: "skill-a",
		ToolDescriptors: []ToolDescriptor{
			{Name: "tool-a1"},
			{Name: "tool-a2"},
		},
	})
	r.Register(&Skill{
		Name: "skill-b",
		ToolDescriptors: []ToolDescriptor{
			{Name: "tool-b1"},
		},
	})

	descs := r.AllToolDescriptors()
	if len(descs) != 3 {
		t.Errorf("expected 3 tool descriptors, got %d", len(descs))
	}
}

func TestRegistry_PromptInjection(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{
		Name:        "test-skill",
		Description: "A test",
		Version:     "0.1.0",
		ToolDescriptors: []ToolDescriptor{
			{Name: "test_tool", Description: "Test tool"},
		},
	})

	injection := r.PromptInjection()
	if injection == "" {
		t.Error("expected prompt injection content")
	}
	if !containsStr(injection, "test-skill") {
		t.Error("injection should contain skill name")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
