package server

import (
	"encoding/json"
	"testing"
)

type mockProvider struct {
	tools []ToolDef
}

func (m *mockProvider) ListTools() []ToolDef { return m.tools }
func (m *mockProvider) CallTool(name string, args map[string]interface{}) (string, error) {
	return "executed " + name, nil
}

func TestServer_Initialize(t *testing.T) {
	p := &mockProvider{}
	s := New(p)

	resp := s.handle(request{JSONRPC: "2.0", Method: "initialize", ID: 1})
	if resp["result"] == nil {
		t.Error("expected result in initialize response")
	}
}

func TestServer_ListTools(t *testing.T) {
	p := &mockProvider{tools: []ToolDef{
		{Name: "test_tool", Description: "a test tool"},
	}}
	s := New(p)

	resp := s.handle(request{JSONRPC: "2.0", Method: "tools/list", ID: 2})
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatal("expected result map")
	}
	if result["tools"] == nil {
		t.Error("expected tools in result")
	}
}

func TestServer_CallTool(t *testing.T) {
	p := &mockProvider{tools: []ToolDef{{Name: "echo"}}}
	s := New(p)

	params, _ := json.Marshal(map[string]interface{}{
		"name":      "echo",
		"arguments": map[string]interface{}{"text": "hi"},
	})

	resp := s.handle(request{JSONRPC: "2.0", Method: "tools/call", Params: params, ID: 3})
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatal("expected result map")
	}
	if result["content"] == nil {
		t.Error("expected content in tool call result")
	}
}
