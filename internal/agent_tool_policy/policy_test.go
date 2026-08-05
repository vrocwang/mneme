package agent_tool_policy

import (
	"testing"
)

func TestDefaultPolicy_AllowsReadAndWrite(t *testing.T) {
	p := DefaultPolicy()
	if !p.IsToolAllowed("read_file", PermReadOnly) {
		t.Error("read_file should be allowed")
	}
	if !p.IsToolAllowed("write_file", PermWrite) {
		t.Error("write_file should be allowed")
	}
	if p.IsToolAllowed("rm_rf_root", PermDangerous) {
		t.Error("dangerous tool should be blocked by default cap")
	}
}

func TestReadOnlyPolicy_BlocksWrite(t *testing.T) {
	p := ReadOnlyPolicy()
	if !p.IsToolAllowed("read_file", PermReadOnly) {
		t.Error("read_file should be allowed in read-only")
	}
	if p.IsToolAllowed("write_file", PermWrite) {
		t.Error("write_file should be blocked in read-only")
	}
}

func TestUnrestrictedPolicy_AllowsEverything(t *testing.T) {
	p := UnrestrictedPolicy()
	if !p.IsToolAllowed("anything", PermDangerous) {
		t.Error("dangerous tool should be allowed in unrestricted")
	}
}

func TestPolicy_Allowlist(t *testing.T) {
	p := &Policy{
		PermissionCap: PermDangerous,
		Allowlist:     []string{"read_file", "write_file"},
	}
	if !p.IsToolAllowed("read_file", PermReadOnly) {
		t.Error("read_file should be allowed")
	}
	if p.IsToolAllowed("shell", PermExecute) {
		t.Error("shell should be blocked (not in allowlist)")
	}
}

func TestPolicy_Denylist(t *testing.T) {
	p := &Policy{
		PermissionCap: PermDangerous,
		Denylist:      []string{"shell", "rm*"},
	}
	if p.IsToolAllowed("shell", PermExecute) {
		t.Error("shell should be blocked by denylist")
	}
	if !p.IsToolAllowed("read_file", PermReadOnly) {
		t.Error("read_file should be allowed")
	}
}

func TestPolicy_WildcardDeny(t *testing.T) {
	p := &Policy{
		PermissionCap: PermDangerous,
		Denylist:      []string{"memory_*"},
	}
	if p.IsToolAllowed("memory_search", PermReadOnly) {
		t.Error("memory_search should be blocked by wildcard")
	}
	if p.IsToolAllowed("memory_save", PermWrite) {
		t.Error("memory_save should be blocked by wildcard")
	}
}

func TestPolicy_NeedsApproval(t *testing.T) {
	p := DefaultPolicy()
	if !p.NeedsApproval(PermDangerous) {
		t.Error("dangerous should need approval by default")
	}
	if p.NeedsApproval(PermReadOnly) {
		t.Error("read_only should not need approval by default")
	}
}

func TestPolicy_FilterTools(t *testing.T) {
	p := &Policy{
		PermissionCap: PermWrite,
		Allowlist:     []string{"read_file", "write_file", "shell"},
	}
	tools := map[string]PermissionLevel{
		"read_file":  PermReadOnly,
		"write_file": PermWrite,
		"shell":      PermExecute,
	}
	allowed := p.FilterTools(tools)
	if len(allowed) != 2 {
		t.Errorf("expected 2 allowed tools (shell cap-blocked), got %d: %v", len(allowed), allowed)
	}
}

func TestRegistry_SetAndGet(t *testing.T) {
	r := NewRegistry()
	r.Set("coder", ReadOnlyPolicy())

	p := r.Get("coder")
	if p == nil {
		t.Fatal("expected policy for coder")
	}
	if !p.IsToolAllowed("read_file", PermReadOnly) {
		t.Error("coder should allow read_file")
	}
	if p.IsToolAllowed("write_file", PermWrite) {
		t.Error("coder with readonly policy should block write_file")
	}

	// Unknown agent returns nil
	if r.Get("unknown") != nil {
		t.Error("expected nil for unknown agent")
	}
}

func TestRegistry_Remove(t *testing.T) {
	r := NewRegistry()
	r.Set("test", DefaultPolicy())
	r.Remove("test")
	if r.Get("test") != nil {
		t.Error("expected nil after remove")
	}
}

func TestParsePermissionLevel(t *testing.T) {
	if ParsePermissionLevel("readonly") != PermReadOnly {
		t.Error("readonly should map to PermReadOnly")
	}
	if ParsePermissionLevel("write") != PermWrite {
		t.Error("write should map to PermWrite")
	}
	if ParsePermissionLevel("dangerous") != PermDangerous {
		t.Error("dangerous should map to PermDangerous")
	}
	if ParsePermissionLevel("bogus") != PermReadOnly {
		t.Error("unknown should default to PermReadOnly")
	}
}

func TestMatchToolName(t *testing.T) {
	if !matchToolName("memory_*", "memory_search") {
		t.Error("wildcard memory_* should match memory_search")
	}
	if !matchToolName("memory_*", "memory_save") {
		t.Error("wildcard memory_* should match memory_save")
	}
	if matchToolName("memory_*", "read_file") {
		t.Error("wildcard memory_* should not match read_file")
	}
	if !matchToolName("read_file", "read_file") {
		t.Error("exact match should work")
	}
}
