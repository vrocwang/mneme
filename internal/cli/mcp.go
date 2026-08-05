package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/mcp/server"
)

func runMCP(args []string) error {
	core, err := bootstrap()
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	srv := server.New(newCapRegProvider(core.CapReg))
	return srv.Run()
}

// capRegProvider adapts CapabilityRegistry to mcp/server.ToolProvider.
type capRegProvider struct {
	reg *capability.CapabilityRegistry
}

func newCapRegProvider(reg *capability.CapabilityRegistry) *capRegProvider {
	return &capRegProvider{reg: reg}
}

func (p *capRegProvider) ListTools() []server.ToolDef {
	descs := p.reg.AllTools()
	defs := make([]server.ToolDef, len(descs))
	for i, d := range descs {
		schema := map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
		if d.InputSchema != nil {
			var parsed map[string]interface{}
			if json.Unmarshal(d.InputSchema, &parsed) == nil {
				schema = parsed
			}
		}
		defs[i] = server.ToolDef{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: schema,
		}
	}
	return defs
}

func (p *capRegProvider) CallTool(name string, args map[string]interface{}) (string, error) {
	result := p.reg.Execute(context.Background(), name, args)
	if result.Error != "" {
		return "", fmt.Errorf("%s", result.Error)
	}
	return result.Output, nil
}
