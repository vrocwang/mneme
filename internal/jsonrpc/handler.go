package jsonrpc

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// ── JSON-RPC 2.0 types ─────────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      *int            `json:"id,omitempty"` // nil = notification
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
	ID      *int        `json:"id,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MethodHandler is a function that handles a JSON-RPC method call.
type MethodHandler func(params json.RawMessage) (interface{}, error)

// MethodRegistry maps method names to handlers.
type MethodRegistry struct {
	mu       sync.RWMutex
	handlers map[string]MethodHandler
}

func newMethodRegistry() *MethodRegistry {
	return &MethodRegistry{handlers: make(map[string]MethodHandler)}
}

// Register adds a method handler.
func (r *MethodRegistry) Register(method string, handler MethodHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[method] = handler
}

func (r *MethodRegistry) get(method string) (MethodHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[method]
	return h, ok
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}

	// Batch support: if the first non-whitespace char is '[', treat as batch.
	if len(body) > 0 && body[0] == '[' {
		s.handleBatch(w, body)
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, nil, -32700, "parse error: "+err.Error())
		return
	}

	resp := s.dispatch(&req)
	if req.ID == nil {
		// Notification — no response.
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleBatch(w http.ResponseWriter, body []byte) {
	var requests []rpcRequest
	if err := json.Unmarshal(body, &requests); err != nil {
		writeRPCError(w, nil, -32700, "parse error: "+err.Error())
		return
	}

	responses := make([]rpcResponse, 0, len(requests))
	for i := range requests {
		resp := s.dispatch(&requests[i])
		if requests[i].ID != nil {
			responses = append(responses, *resp)
		}
	}

	if len(responses) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responses)
}

func (s *Server) dispatch(req *rpcRequest) *rpcResponse {
	handler, ok := s.registry.get(req.Method)
	if !ok {
		return &rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
			ID:      req.ID,
		}
	}

	result, err := handler(req.Params)
	if err != nil {
		return &rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32000, Message: err.Error()},
			ID:      req.ID,
		}
	}

	return &rpcResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      req.ID,
	}
}

func writeRPCError(w http.ResponseWriter, id *int, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(rpcResponse{
		JSONRPC: "2.0",
		Error:   &rpcError{Code: code, Message: msg},
		ID:      id,
	})
}

// RegisterMethod exposes a method on the JSON-RPC endpoint.
func (s *Server) RegisterMethod(method string, handler MethodHandler) {
	s.registry.Register(method, handler)
}
