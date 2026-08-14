// Package capability provides a unified registry and eino bridge for all
// tools — Go builtins, MCP servers, and extensions.
package capability

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	jsonschema "github.com/eino-contrib/jsonschema"
)

// regToolAdapter wraps a single CapabilityRegistry tool as an eino
// tool.InvokableTool. It bridges the registry.Execute(name, args) call
// into eino's native tool execution pipeline so MCP, extension, and
// any other registry-registered tools are visible to the General Agent.
type regToolAdapter struct {
	reg      *CapabilityRegistry
	toolName string
	info     *schema.ToolInfo
}

// NewRegistryToolAdapter creates an eino tool.BaseTool that delegates
// execution to the CapabilityRegistry for a single tool by name.
// Returns nil when the tool is not found in the registry.
func NewRegistryToolAdapter(reg *CapabilityRegistry, toolName string) tool.BaseTool {
	t, ok := reg.GetTool(toolName)
	if !ok {
		return nil
	}

	st := t.Schema()
	params := convertSchemaToParams(st.Parameters)

	return &regToolAdapter{
		reg:      reg,
		toolName: toolName,
		info: &schema.ToolInfo{
			Name:        st.Name,
			Desc:        st.Description,
			ParamsOneOf: params,
		},
	}
}

// Info returns the tool metadata for eino's tool registry.
func (a *regToolAdapter) Info(_ context.Context) (*schema.ToolInfo, error) {
	return a.info, nil
}

// InvokableRun implements tool.InvokableTool. It deserialises the
// JSON arguments string, routes execution through the CapabilityRegistry
// (which dispatches local, MCP, or extension tools), and returns the
// result as eino expects.
func (a *regToolAdapter) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args map[string]interface{}
	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
			return "", fmt.Errorf("registry tool %q: invalid arguments: %w", a.toolName, err)
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}

	result := a.reg.Execute(ctx, a.toolName, args)
	if result.Error != "" {
		return "", fmt.Errorf("registry tool %q: %s", a.toolName, result.Error)
	}
	if !result.Success && result.Output != "" {
		return result.Output, fmt.Errorf("registry tool %q: %s", a.toolName, result.Error)
	}
	return result.Output, nil
}

// CollectRegistryTools returns eino tool.BaseTool adapters for every tool
// currently registered in the CapabilityRegistry. The registry is the single
// source of truth for tools (builtin bundles, extensions, MCP, memory, and
// config tools); the caller may pass skipNames to exclude specific tools.
func CollectRegistryTools(reg *CapabilityRegistry, skipNames map[string]bool) []tool.BaseTool {
	if reg == nil {
		return nil
	}

	names := reg.ToolNames()
	out := make([]tool.BaseTool, 0, len(names))

	for _, name := range names {
		if skipNames[name] {
			continue
		}
		a := NewRegistryToolAdapter(reg, name)
		if a != nil {
			out = append(out, a)
		}
	}
	return out
}

// convertSchemaToParams converts a CapReg-style JSON Schema parameters
// map into eino's schema.ParamsOneOf.
func convertSchemaToParams(params map[string]interface{}) *schema.ParamsOneOf {
	if params == nil {
		return nil
	}

	if props, ok := params["properties"].(map[string]interface{}); ok && len(props) > 0 {
		// Marshal the full JSON Schema and parse into jsonschema.Schema.
		schemaJSON, err := json.Marshal(params)
		if err != nil {
			return nil
		}
		var js jsonschema.Schema
		if err := json.Unmarshal(schemaJSON, &js); err != nil {
			return nil
		}
		return schema.NewParamsOneOfByJSONSchema(&js)
	}

	// Empty or type-only schema — no parameters needed.
	return nil
}

// Ensure the adapter satisfies eino's InvokableTool interface at compile time.
var _ tool.InvokableTool = (*regToolAdapter)(nil)
