package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
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

// HTTPServer is an HTTP/SSE transport wrapper for the MCP server.
// It implements http.Handler and supports the MCP Streamable HTTP transport.
type HTTPServer struct {
	server   *Server
	log      *slog.Logger
	sessions map[string]*sseSession
	mu       sync.Mutex
}

type sseSession struct {
	ch   chan map[string]interface{}
	done <-chan struct{}
}

// NewHTTPServer creates an HTTP handler for MCP over HTTP/SSE transport.
func NewHTTPServer(log *slog.Logger, provider ToolProvider) *HTTPServer {
	if log == nil {
		log = slog.Default()
	}
	return &HTTPServer{
		server:   New(provider),
		log:      log,
		sessions: make(map[string]*sseSession),
	}
}

// ServeHTTP routes HTTP requests for the MCP Streamable HTTP transport.
func (h *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Mcp-Session-Id")

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/message":
		h.handleMessage(w, r, sessionID)
	case r.Method == http.MethodGet && r.URL.Path == "/sse":
		h.handleSSE(w, r, sessionID)
	default:
		http.NotFound(w, r)
	}
}

func (h *HTTPServer) handleMessage(w http.ResponseWriter, r *http.Request, sessionID string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON-RPC", http.StatusBadRequest)
		return
	}

	resp := h.server.handle(req)

	// If an SSE session is active for this session ID and method is a
	// notification (no id field), push to the SSE channel instead.
	if req.ID == 0 && sessionID != "" {
		h.mu.Lock()
		sess, ok := h.sessions[sessionID]
		h.mu.Unlock()
		if ok {
			select {
			case sess.ch <- resp:
			default:
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if sessionID != "" {
		w.Header().Set("Mcp-Session-Id", sessionID)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if req.ID == 0 {
		// JSON-RPC notification: no response expected
		w.WriteHeader(http.StatusAccepted)
		return
	}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "failed to marshal response", http.StatusInternalServerError)
		return
	}
	w.Write(respJSON)
}

func (h *HTTPServer) handleSSE(w http.ResponseWriter, r *http.Request, sessionID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Generate a session ID if not provided.
	if sessionID == "" {
		h.mu.Lock()
		sessionID = fmt.Sprintf("mcp-sess-%d", len(h.sessions)+1)
		h.mu.Unlock()
	}

	ch := make(chan map[string]interface{}, 64)
	done := r.Context().Done()

	ss := &sseSession{ch: ch, done: done}
	h.mu.Lock()
	h.sessions[sessionID] = ss
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.sessions, sessionID)
		h.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Mcp-Session-Id", sessionID)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	flusher.Flush()

	// Send the endpoint event per MCP Streamable HTTP spec.
	fmt.Fprintf(w, "event: endpoint\ndata: /message?sessionId=%s\n\n", sessionID)
	flusher.Flush()

	// Keep the connection open and forward server→client notifications.
	for {
		select {
		case <-done:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// Notify sends a JSON-RPC notification to all connected SSE sessions.
func (h *HTTPServer) Notify(method string, params map[string]interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sess := range h.sessions {
		select {
		case sess.ch <- msg:
		default:
		}
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
