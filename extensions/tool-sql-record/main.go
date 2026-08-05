// SQL Record extension for Mneme.
//
// Provides:
//   - insert_sql_record: insert records into the local SQLite database
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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// dataDir returns the host workspace directory.
func dataDir() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "data")
}

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
	Name:        "tool-sql-record",
	Version:     "0.1.0",
	Description: "Insert records into the local Mneme SQLite database",
	Tools:       []string{"insert_sql_record"},
	AgentDefs:   []string{},
	ProtocolMin: 1,
}

var toolDefs = []toolDef{
	{
		Name:        "insert_sql_record",
		Description: "Insert a record into a table in the local Mneme SQLite database (mneme.db). Columns and values are derived from the data map.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"table": map[string]interface{}{"type": "string", "description": "Target table name"},
				"data":  map[string]interface{}{"type": "object", "description": "Map of column names to values to insert"},
			},
			"required": []string{"table", "data"},
		},
		Permission: "execute",
		HasEffects: true,
	},
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log.Info("tool-sql-record extension starting")
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
		var req rpcRequest
		json.Unmarshal(line, &req)
		resp := handleRequest(&req)
		respBytes, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stdout, "%s\n", respBytes)
	}
}

func handleRequest(req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "extension.describe":
		result, _ := json.Marshal(extManifest)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	case "extension.list_tools":
		type lr struct{ Tools []toolDef }
		result, _ := json.Marshal(lr{Tools: toolDefs})
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
		case "insert_sql_record":
			result = insertSQLRecord(ctx, params.Args)
		default:
			result = callToolResult{Error: fmt.Sprintf("unknown: %s", params.Name)}
		}
		resultBytes, _ := json.Marshal(result)
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown: %s", req.Method)}}
	}
}

func dbPath() string {
	if p := os.Getenv("MNEME_DB_PATH"); p != "" {
		return p
	}
	return filepath.Join(dataDir(), "mneme.db")
}

func insertSQLRecord(ctx context.Context, args map[string]interface{}) callToolResult {
	table, _ := args["table"].(string)
	if table == "" {
		return callToolResult{Error: "table is required"}
	}
	if !isValidIdentifier(table) {
		return callToolResult{Error: "table must be a valid identifier (letters, digits, underscore; must start with a letter or underscore)"}
	}

	rawData, ok := args["data"]
	if !ok {
		return callToolResult{Error: "data is required"}
	}

	data, ok := rawData.(map[string]interface{})
	if !ok {
		return callToolResult{Error: "data must be an object/map of column:value pairs"}
	}

	if len(data) == 0 {
		return callToolResult{Error: "data map is empty"}
	}

	dbPath := dbPath()
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	// Build INSERT statement
	var columns []string
	var valueParts []string

	for col, val := range data {
		if !isValidIdentifier(col) {
			return callToolResult{Error: fmt.Sprintf("invalid column name: %q", col)}
		}
		safeCol := strings.ReplaceAll(col, `"`, `""`)
		columns = append(columns, fmt.Sprintf(`"%s"`, safeCol))
		valueParts = append(valueParts, sqliteValue(val))
	}

	sqlStmt := fmt.Sprintf("INSERT INTO \"%s\" (%s) VALUES (%s);\n",
		table,
		strings.Join(columns, ", "),
		strings.Join(valueParts, ", "))

	cmd := exec.CommandContext(ctx, "sqlite3", dbPath)
	cmd.Stdin = strings.NewReader(sqlStmt)

	output, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(output))

	if err != nil {
		if outStr != "" {
			return callToolResult{Error: fmt.Sprintf("sqlite error: %s", outStr)}
		}
		return callToolResult{Error: fmt.Sprintf("sqlite3: %v (is sqlite3 installed?)", err)}
	}

	out := map[string]interface{}{
		"table":     table,
		"columns":   len(data),
		"statement": sqlStmt,
		"db_path":   dbPath,
	}
	if outStr != "" {
		out["output"] = outStr
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return callToolResult{Success: true, Output: string(b)}
}

// sqliteValue formats a Go value as a SQLite literal for safe embedding in SQL.
func sqliteValue(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return "NULL"
	case bool:
		if val {
			return "1"
		}
		return "0"
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case string:
		escaped := strings.ReplaceAll(val, "'", "''")
		return fmt.Sprintf("'%s'", escaped)
	case map[string]interface{}, []interface{}:
		b, _ := json.Marshal(val)
		escaped := strings.ReplaceAll(string(b), "'", "''")
		return fmt.Sprintf("'%s'", escaped)
	default:
		escaped := strings.ReplaceAll(fmt.Sprintf("%v", val), "'", "''")
		return fmt.Sprintf("'%s'", escaped)
	}
}

// isValidIdentifier validates that a name is a safe SQL identifier
// (letters, digits, underscore; must start with a letter or underscore).
// This prevents SQL injection via table/column names sourced from tool args.
func isValidIdentifier(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for i, r := range name {
		if r == '_' {
			continue
		}
		if i == 0 && !unicode.IsLetter(r) {
			return false
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
