package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// ToolProvider is the interface for providing tools to the MCP server.
type ToolProvider interface {
	ListTools() []ToolDef
	CallTool(name string, args map[string]interface{}) (string, error)
}

// ToolDef describes a tool for MCP.
type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// Server is an MCP JSON-RPC server over stdio.
type Server struct {
	provider ToolProvider
}

// New creates an MCP server.
func New(provider ToolProvider) *Server {
	return &Server{provider: provider}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      int             `json:"id"`
}

// Run starts the MCP server on stdin/stdout (stdio transport).
func (s *Server) Run() error {
	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		resp := s.handle(req)
		respJSON, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("marshal mcp response: %w", err)
		}
		fmt.Fprintf(writer, "%s\n", respJSON)
	}
}

func (s *Server) handle(req request) map[string]interface{} {
	switch req.Method {
	case "initialize":
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "mneme-go",
					"version": "0.1.0",
				},
			},
		}

	case "tools/list":
		tools := s.provider.ListTools()
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]interface{}{
				"tools": tools,
			},
		}

	case "tools/call":
		var call struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &call); err != nil {
			return errorResp(req.ID, fmt.Sprintf("invalid params: %v", err))
		}

		output, err := s.provider.CallTool(call.Name, call.Arguments)
		if err != nil {
			return map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"content": []map[string]interface{}{
						{"type": "text", "text": err.Error()},
					},
					"isError": true,
				},
			}
		}

		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": output},
				},
			},
		}

	default:
		return errorResp(req.ID, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func errorResp(id int, msg string) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    -32601,
			"message": msg,
		},
	}
}
