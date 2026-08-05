// LSP extension for Mneme.
//
// Provides Language Server Protocol integration tools:
//   - lsp_definition: go to definition
//   - lsp_references: find all references
//   - lsp_hover: get hover information
//   - lsp_symbols: list document/workspace symbols
//   - lsp_complete: get completions at a position
//
// Uses stdin/stdout JSON-RPC to communicate with both the Mneme core
// AND the language server process (spawned as a subprocess).
//
// Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ── JSON-RPC types ────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type manifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	AgentDefs   []string `json:"agent_defs"`
	ProtocolMin int      `json:"protocol_min"`
}

type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Permission  string                 `json:"permission"`
	HasEffects  bool                   `json:"has_effects"`
}

type callToolParams struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type callToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

var extManifest = manifest{
	Name:        "lsp",
	Version:     "0.1.0",
	Description: "Language Server Protocol integration: definition, references, hover, symbols, completions",
	Tools:       []string{"lsp_definition", "lsp_references", "lsp_hover", "lsp_symbols", "lsp_complete"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "lsp_definition",
		Description: "Go to the definition of a symbol at the given file and position",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filePath":   map[string]interface{}{"type": "string", "description": "Absolute path to the source file"},
				"line":       map[string]interface{}{"type": "number", "description": "Line number (1-based)"},
				"character":  map[string]interface{}{"type": "number", "description": "Character offset (1-based)"},
				"languageId": map[string]interface{}{"type": "string", "description": "Language ID: go, typescript, python, rust, etc. Auto-detected if empty."},
			},
			"required": []string{"filePath", "line", "character"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "lsp_references",
		Description: "Find all references to a symbol at the given file and position",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filePath":    map[string]interface{}{"type": "string", "description": "Absolute path to the source file"},
				"line":        map[string]interface{}{"type": "number", "description": "Line number (1-based)"},
				"character":   map[string]interface{}{"type": "number", "description": "Character offset (1-based)"},
				"includeDecl": map[string]interface{}{"type": "boolean", "description": "Include declaration in results (default true)"},
			},
			"required": []string{"filePath", "line", "character"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "lsp_hover",
		Description: "Get hover information (type, documentation) for a symbol at the given position",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filePath":  map[string]interface{}{"type": "string", "description": "Absolute path to the source file"},
				"line":      map[string]interface{}{"type": "number", "description": "Line number (1-based)"},
				"character": map[string]interface{}{"type": "number", "description": "Character offset (1-based)"},
			},
			"required": []string{"filePath", "line", "character"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "lsp_symbols",
		Description: "List document symbols or search workspace symbols",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filePath": map[string]interface{}{"type": "string", "description": "File path for document symbols. Omit for workspace symbol search."},
				"query":    map[string]interface{}{"type": "string", "description": "Search query for workspace symbols"},
				"maxItems": map[string]interface{}{"type": "number", "description": "Max results (default 50)"},
			},
			"required": []string{},
		},
		Permission: "read_only",
		HasEffects: false,
	},
	{
		Name:        "lsp_complete",
		Description: "Get code completion suggestions at a position",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filePath":  map[string]interface{}{"type": "string", "description": "Absolute path to the source file"},
				"line":      map[string]interface{}{"type": "number", "description": "Line number (1-based)"},
				"character": map[string]interface{}{"type": "number", "description": "Character offset (1-based)"},
				"maxItems":  map[string]interface{}{"type": "number", "description": "Max completions (default 20)"},
			},
			"required": []string{"filePath", "line", "character"},
		},
		Permission: "read_only",
		HasEffects: false,
	},
}

// ── LSP client state ──────────────────────────────────────────────

type lspClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	reqID  int64
	log    *slog.Logger
}

var (
	clientCache   = make(map[string]*lspClient)
	clientCacheMu sync.Mutex
)

// lspsByLang maps language IDs to LSP server commands.
var lspsByLang = map[string][]string{
	"go":         {"gopls", "serve"},
	"typescript": {"typescript-language-server", "--stdio"},
	"javascript": {"typescript-language-server", "--stdio"},
	"python":     {"pyright-langserver", "--stdio"},
	"rust":       {"rust-analyzer"},
	"c":          {"clangd"},
	"cpp":        {"clangd"},
	"lua":        {"lua-language-server"},
	"bash":       {"bash-language-server", "start"},
	"json":       {"vscode-json-languageserver", "--stdio"},
	"html":       {"vscode-html-languageserver", "--stdio"},
	"css":        {"vscode-css-languageserver", "--stdio"},
}

func detectLangID(filePath string) string {
	ext := filepath.Ext(filePath)
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".lua":
		return "lua"
	case ".sh", ".bash":
		return "bash"
	case ".json":
		return "json"
	case ".html":
		return "html"
	case ".css":
		return "css"
	default:
		return ""
	}
}

func getOrStartLS(ctx context.Context, langID string, log *slog.Logger) (*lspClient, error) {
	clientCacheMu.Lock()
	if c, ok := clientCache[langID]; ok {
		// Check if the cached process is still alive.
		if c.cmd.Process != nil {
			// Sending signal 0 checks if the process exists.
			if err := c.cmd.Process.Signal(syscall.Signal(0)); err == nil {
				clientCacheMu.Unlock()
				return c, nil
			}
		}
		// Process is dead, remove from cache and restart.
		delete(clientCache, langID)
	}
	clientCacheMu.Unlock()
	// Lock again after the unlock to start fresh
	clientCacheMu.Lock()
	defer clientCacheMu.Unlock()

	// Double-check: might have been started while we released the lock.
	if c, ok := clientCache[langID]; ok {
		if c.cmd.Process != nil {
			if err := c.cmd.Process.Signal(syscall.Signal(0)); err == nil {
				return c, nil
			}
			delete(clientCache, langID)
		}
	}

	serverArgs, ok := lspsByLang[langID]
	if !ok {
		return nil, fmt.Errorf("no LSP server configured for language: %s", langID)
	}

	cmd := exec.CommandContext(ctx, serverArgs[0], serverArgs[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start LSP server %s: %w", serverArgs[0], err)
	}

	c := &lspClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdoutPipe),
		reqID:  1,
		log:    log,
	}

	// Send initialize request
	initID := c.nextID()
	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      initID,
		"method":  "initialize",
		"params": map[string]interface{}{
			"processId": os.Getpid(),
			"rootUri":   nil,
			"capabilities": map[string]interface{}{
				"textDocument": map[string]interface{}{
					"definition":     map[string]interface{}{"linkSupport": true},
					"references":     map[string]interface{}{},
					"hover":          map[string]interface{}{"contentFormat": []string{"plaintext", "markdown"}},
					"completion":     map[string]interface{}{},
					"documentSymbol": map[string]interface{}{"hierarchicalDocumentSymbolSupport": true},
				},
				"workspace": map[string]interface{}{
					"symbol": map[string]interface{}{},
				},
			},
		},
	}

	reqBytes, _ := json.Marshal(initReq)
	c.stdin.Write(append(reqBytes, '\n'))

	// Send initialized notification
	initialized := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	}
	notifBytes, _ := json.Marshal(initialized)
	c.stdin.Write(append(notifBytes, '\n'))

	// Read initialize response
	respLine, err := c.stdout.ReadBytes('\n')
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("LSP initialize response: %w", err)
	}

	var resp struct {
		ID     int64           `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	json.Unmarshal(respLine, &resp)
	if resp.Error != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("LSP initialize failed: %s", resp.Error.Message)
	}

	clientCacheMu.Lock()
	clientCache[langID] = c
	clientCacheMu.Unlock()
	log.Info("LSP server connected", "lang", langID, "server", serverArgs[0])
	return c, nil
}

func (c *lspClient) nextID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.reqID
	c.reqID++
	return id
}

func (c *lspClient) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.reqID
	c.reqID++
	c.mu.Unlock()

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	if _, err := c.stdin.Write(append(reqBytes, '\n')); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	respLine, err := c.stdout.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	var resp struct {
		ID     int64           `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(respLine, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("LSP error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}
	return resp.Result, nil
}

// ── Main ──────────────────────────────────────────────────────────

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("lsp extension starting", "version", extManifest.Version)

	reader := bufio.NewReader(os.Stdin)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				log.Info("stdin closed, exiting")
				return
			}
			log.Error("read error", "err", err)
			return
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			log.Error("unmarshal error", "err", err)
			continue
		}

		resp := handleRequest(&req, log)
		respBytes, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stdout, "%s\n", respBytes)
	}
}

func handleRequest(req *rpcRequest, log *slog.Logger) *rpcResponse {
	switch req.Method {
	case "extension.describe":
		result, _ := json.Marshal(extManifest)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_tools":
		type listResult struct {
			Tools []toolDef `json:"tools"`
		}
		result, _ := json.Marshal(listResult{Tools: toolDefs})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_agents":
		result, _ := json.Marshal(map[string]interface{}{"agents": []interface{}{}})
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.call_tool":
		var params callToolParams
		json.Unmarshal(req.Params, &params)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var result callToolResult
		switch params.Name {
		case "lsp_definition":
			result = lspDefinition(ctx, params.Args, log)
		case "lsp_references":
			result = lspReferences(ctx, params.Args, log)
		case "lsp_hover":
			result = lspHover(ctx, params.Args, log)
		case "lsp_symbols":
			result = lspSymbols(ctx, params.Args, log)
		case "lsp_complete":
			result = lspComplete(ctx, params.Args, log)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown tool: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

// ── Tool implementations ──────────────────────────────────────────

func lspDefinition(ctx context.Context, args map[string]interface{}, log *slog.Logger) callToolResult {
	filePath, _ := args["filePath"].(string)
	line := intFromArgs(args, "line")
	character := intFromArgs(args, "character")

	langID := getStrArg(args, "languageId", "")
	if langID == "" {
		langID = detectLangID(filePath)
	}
	if langID == "" {
		return callToolResult{Error: "cannot detect language; provide languageId"}
	}

	c, err := getOrStartLS(ctx, langID, log)
	if err != nil {
		return callToolResult{Error: err.Error()}
	}

	result, err := c.call(ctx, "textDocument/definition", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": fileToURI(filePath)},
		"position":     map[string]int{"line": line - 1, "character": character - 1},
	})
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("definition: %v", err)}
	}

	return callToolResult{Success: true, Output: prettyJSON(result)}
}

func lspReferences(ctx context.Context, args map[string]interface{}, log *slog.Logger) callToolResult {
	filePath, _ := args["filePath"].(string)
	line := intFromArgs(args, "line")
	character := intFromArgs(args, "character")
	includeDecl := true
	if id, ok := args["includeDecl"].(bool); ok {
		includeDecl = id
	}

	langID := getStrArg(args, "languageId", detectLangID(filePath))
	if langID == "" {
		return callToolResult{Error: "cannot detect language; provide languageId"}
	}

	c, err := getOrStartLS(ctx, langID, log)
	if err != nil {
		return callToolResult{Error: err.Error()}
	}

	result, err := c.call(ctx, "textDocument/references", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": fileToURI(filePath)},
		"position":     map[string]int{"line": line - 1, "character": character - 1},
		"context":      map[string]bool{"includeDeclaration": includeDecl},
	})
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("references: %v", err)}
	}

	return callToolResult{Success: true, Output: prettyJSON(result)}
}

func lspHover(ctx context.Context, args map[string]interface{}, log *slog.Logger) callToolResult {
	filePath, _ := args["filePath"].(string)
	line := intFromArgs(args, "line")
	character := intFromArgs(args, "character")

	langID := getStrArg(args, "languageId", detectLangID(filePath))
	if langID == "" {
		return callToolResult{Error: "cannot detect language; provide languageId"}
	}

	c, err := getOrStartLS(ctx, langID, log)
	if err != nil {
		return callToolResult{Error: err.Error()}
	}

	result, err := c.call(ctx, "textDocument/hover", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": fileToURI(filePath)},
		"position":     map[string]int{"line": line - 1, "character": character - 1},
	})
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("hover: %v", err)}
	}

	return callToolResult{Success: true, Output: extractHoverText(result)}
}

func lspSymbols(ctx context.Context, args map[string]interface{}, log *slog.Logger) callToolResult {
	filePath, _ := args["filePath"].(string)
	query, _ := args["query"].(string)

	var langID string
	if filePath != "" {
		langID = detectLangID(filePath)
	}

	var result json.RawMessage
	var err error

	if filePath != "" {
		// Document symbols
		if langID == "" {
			return callToolResult{Error: "cannot detect language from file path"}
		}
		c, startErr := getOrStartLS(ctx, langID, log)
		if startErr != nil {
			return callToolResult{Error: startErr.Error()}
		}
		result, err = c.call(ctx, "textDocument/documentSymbol", map[string]interface{}{
			"textDocument": map[string]interface{}{"uri": fileToURI(filePath)},
		})
	} else if query != "" {
		// Workspace symbols — use first available LSP (sorted for determinism)
		langs := make([]string, 0, len(lspsByLang))
		for lang := range lspsByLang {
			langs = append(langs, lang)
		}
		sort.Strings(langs)
		for _, lang := range langs {
			c, startErr := getOrStartLS(ctx, lang, log)
			if startErr != nil {
				continue
			}
			result, err = c.call(ctx, "workspace/symbol", map[string]interface{}{
				"query": query,
			})
			break
		}
		if result == nil {
			return callToolResult{Error: "no LSP server available for workspace symbol search"}
		}
	} else {
		return callToolResult{Error: "provide either filePath (document symbols) or query (workspace symbols)"}
	}

	if err != nil {
		return callToolResult{Error: fmt.Sprintf("symbols: %v", err)}
	}

	return callToolResult{Success: true, Output: prettyJSON(result)}
}

func lspComplete(ctx context.Context, args map[string]interface{}, log *slog.Logger) callToolResult {
	filePath, _ := args["filePath"].(string)
	line := intFromArgs(args, "line")
	character := intFromArgs(args, "character")

	langID := getStrArg(args, "languageId", detectLangID(filePath))
	if langID == "" {
		return callToolResult{Error: "cannot detect language; provide languageId"}
	}

	c, err := getOrStartLS(ctx, langID, log)
	if err != nil {
		return callToolResult{Error: err.Error()}
	}

	result, err := c.call(ctx, "textDocument/completion", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": fileToURI(filePath)},
		"position":     map[string]int{"line": line - 1, "character": character - 1},
	})
	if err != nil {
		return callToolResult{Error: fmt.Sprintf("completion: %v", err)}
	}

	return callToolResult{Success: true, Output: prettyJSON(result)}
}

// ── Helpers ────────────────────────────────────────────────────────

func intFromArgs(args map[string]interface{}, key string) int {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func intFromOptArgs(args map[string]interface{}, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

func getStrArg(args map[string]interface{}, key, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func fileToURI(path string) string {
	abs, _ := filepath.Abs(path)
	// Use filepath.SplitList-like logic: split on OS separator, percent-encode each segment.
	parts := strings.Split(abs, string(filepath.Separator))
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	result := "file://" + strings.Join(parts, "/")
	// On Windows, ensure exactly three slashes: file:///C:/...
	if len(result) > 7 && result[7] != '/' {
		result = "file:///" + strings.Join(parts, "/")
	}
	return result
}

func prettyJSON(data json.RawMessage) string {
	var v interface{}
	json.Unmarshal(data, &v)
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(data)
	}
	return string(pretty)
}

func extractHoverText(data json.RawMessage) string {
	var hover struct {
		Contents struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		} `json:"contents"`
	}
	json.Unmarshal(data, &hover)
	if hover.Contents.Value != "" {
		return hover.Contents.Value
	}
	return prettyJSON(data)
}
