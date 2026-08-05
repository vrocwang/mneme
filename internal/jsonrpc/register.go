package jsonrpc

import "encoding/json"

// AppMethods is the interface that the main App satisfies for JSON-RPC method
// registration. This keeps the jsonrpc package decoupled from the App type.
type AppMethods interface {
	Health() map[string]interface{}
	ListAgents() []map[string]interface{}
	SearchMemory(query string) (string, error)
}

// RegisterAppMethods wires the Wails App methods into the JSON-RPC server.
// All handler logic lives here — app.go remains a thin proxy.
func RegisterAppMethods(s *Server, app AppMethods) {
	s.RegisterMethod("health.check", func(params json.RawMessage) (interface{}, error) {
		return app.Health(), nil
	})

	s.RegisterMethod("agent.list", func(params json.RawMessage) (interface{}, error) {
		return app.ListAgents(), nil
	})

	s.RegisterMethod("memory.search", func(params json.RawMessage) (interface{}, error) {
		var p struct{ Query string }
		if len(params) > 0 {
			json.Unmarshal(params, &p)
		}
		return app.SearchMemory(p.Query)
	})
}
