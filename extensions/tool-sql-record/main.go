// SQL Record extension for Mneme.
//
// Provides:
//   - insert_sql_record: insert records into the local SQLite database
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/simon/mneme/pkg/extsdk"
)

// dataDir returns the host workspace directory.
func dataDir() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "data")
}

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "tool-sql-record",
		Version:     "0.1.0",
		Description: "Insert records into the local Mneme SQLite database",
	})

	srv.RegisterTool(extsdk.ToolDef{
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
	}, insertSQLRecord)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tool-sql-record: %v\n", err)
		os.Exit(1)
	}
}

func dbPath() string {
	if p := os.Getenv("MNEME_DB_PATH"); p != "" {
		return p
	}
	return filepath.Join(dataDir(), "mneme.db")
}

func insertSQLRecord(ctx context.Context, args map[string]interface{}) extsdk.Result {
	table, _ := args["table"].(string)
	if table == "" {
		return extsdk.Result{Error: "table is required"}
	}
	if !isValidIdentifier(table) {
		return extsdk.Result{Error: "table must be a valid identifier (letters, digits, underscore; must start with a letter or underscore)"}
	}

	rawData, ok := args["data"]
	if !ok {
		return extsdk.Result{Error: "data is required"}
	}

	data, ok := rawData.(map[string]interface{})
	if !ok {
		return extsdk.Result{Error: "data must be an object/map of column:value pairs"}
	}

	if len(data) == 0 {
		return extsdk.Result{Error: "data map is empty"}
	}

	dbPath := dbPath()
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	// Build INSERT statement
	var columns []string
	var valueParts []string

	for col, val := range data {
		if !isValidIdentifier(col) {
			return extsdk.Result{Error: fmt.Sprintf("invalid column name: %q", col)}
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
			return extsdk.Result{Error: fmt.Sprintf("sqlite error: %s", outStr)}
		}
		return extsdk.Result{Error: fmt.Sprintf("sqlite3: %v (is sqlite3 installed?)", err)}
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
	return extsdk.Result{Success: true, Output: string(b)}
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
