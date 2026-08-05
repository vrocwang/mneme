// Package runtime manages external language runtimes (Node.js, Python, Ruby)
// as JSON-RPC subprocesses. The Bridge interface standardizes how runtime tools
// are discovered and invoked via the extension protocol.
package runtime

import "context"

// ToolDefinition describes a tool exposed by a runtime bridge.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Result is the result of executing a tool via a runtime bridge.
type Result struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// Bridge is the interface for external language runtimes. Each implementation
// manages a subprocess (Node.js, Python, Ruby, etc.) that communicates via
// stdin/stdout JSON-RPC using the extension protocol.
//
// Implementations live in extensions/ and are loaded at startup by the extension
// discovery mechanism. The core never imports language-specific runtimes directly.
type Bridge interface {
	// ListTools returns the tools exposed by the runtime.
	ListTools(ctx context.Context) ([]ToolDefinition, error)

	// Execute runs a named tool with the given arguments inside the runtime.
	Execute(ctx context.Context, toolName string, args map[string]interface{}) (Result, error)

	// Stop terminates the runtime subprocess.
	Stop() error
}
