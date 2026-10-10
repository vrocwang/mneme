package capability

import (
	"encoding/json"
	"testing"
)

// TestGetToolDiagnostics_SplitsMCPByTransport is the SET 2.11 test: the
// mcpStdioTools key must count stdio transports only, with the HTTP transports
// accounted for under the new mcpHTTPTools key.
func TestGetToolDiagnostics_SplitsMCPByTransport(t *testing.T) {
	reg := NewCapabilityRegistry()
	rpc := NewCapabilityRPC(reg)

	stdio := NewMCPServerSet(ServerEntry{Name: "stdio-srv", Transport: "stdio", Command: "server-bin", Enabled: true})
	stdio.Tools = []ToolDescriptor{{Name: "stdio_a"}, {Name: "stdio_b"}}

	httpSet := NewMCPServerSet(ServerEntry{Name: "http-srv", Transport: "http", URL: "https://mcp.example/rpc", Enabled: true})
	httpSet.Tools = []ToolDescriptor{{Name: "http_a"}, {Name: "http_b"}, {Name: "http_c"}}

	// A legacy MCP set with no config: it has no transport to read, so it must
	// keep the historical stdio classification rather than jumping to HTTP.
	legacy := &CapabilitySet{
		ID: MCPSetID("legacy"), Name: "legacy", Kind: KindMCPServer, Enabled: true,
		Tools: []ToolDescriptor{{Name: "legacy_a"}},
	}

	// An extension set must still be counted as JSON-RPC and must not leak into
	// either MCP bucket.
	ext := &CapabilitySet{
		ID: ExtensionSetID("demo"), Name: "demo", Kind: KindExtension, Enabled: true,
		Tools: []ToolDescriptor{{Name: "ext_a"}},
	}

	for _, set := range []*CapabilitySet{stdio, httpSet, legacy, ext} {
		if err := reg.AddSet(set); err != nil {
			t.Fatalf("AddSet(%s): %v", set.ID, err)
		}
	}

	diag := rpc.GetToolDiagnostics()

	assertInt := func(key string, want int) {
		t.Helper()
		v, ok := diag[key]
		if !ok {
			t.Fatalf("GetToolDiagnostics() missing key %q: %+v", key, diag)
		}
		got, ok := v.(int)
		if !ok {
			t.Fatalf("GetToolDiagnostics()[%q] is %T, want int", key, v)
		}
		if got != want {
			t.Errorf("GetToolDiagnostics()[%q] = %d, want %d", key, got, want)
		}
	}

	if ok, _ := diag["ok"].(bool); !ok {
		t.Errorf("GetToolDiagnostics()[ok] = %v, want true", diag["ok"])
	}
	assertInt("mcpStdioTools", 3) // stdio-srv (2) + legacy (1)
	assertInt("mcpHTTPTools", 3)  // http-srv (3)
	assertInt("jsonRpcTools", 1)  // demo extension (1)
	assertInt("enabledTools", 7)  // 2 + 3 + 1 + 1
	assertInt("totalTools", 0)    // no executable tools registered in this test

	bySource, ok := diag["bySource"].(map[string]int)
	if !ok {
		t.Fatalf("GetToolDiagnostics()[bySource] is %T, want map[string]int", diag["bySource"])
	}
	if bySource[MCPSetID("http-srv")] != 3 {
		t.Errorf("bySource[%s] = %d, want 3", MCPSetID("http-srv"), bySource[MCPSetID("http-srv")])
	}
}

// TestMCPSetTransport covers the transport reader used by the split, including
// the malformed-config path.
func TestMCPSetTransport(t *testing.T) {
	tests := []struct {
		name string
		set  *CapabilitySet
		want string
	}{
		{"nil set", nil, "stdio"},
		{"empty config", &CapabilitySet{ID: "mcp:x"}, "stdio"},
		{"malformed config", &CapabilitySet{ID: "mcp:y", Config: json.RawMessage(`{not json`)}, "stdio"},
		{"empty transport field", &CapabilitySet{ID: "mcp:z", Config: json.RawMessage(`{"name":"z"}`)}, "stdio"},
		{"stdio", NewMCPServerSet(ServerEntry{Name: "a", Transport: "stdio"}), "stdio"},
		{"http", NewMCPServerSet(ServerEntry{Name: "b", Transport: "http"}), "http"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mcpSetTransport(tt.set); got != tt.want {
				t.Errorf("mcpSetTransport() = %q, want %q", got, tt.want)
			}
		})
	}
}
