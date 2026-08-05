package memory

import (
	"database/sql"
	"fmt"
	"strings"
)

// NamespaceManager handles namespace lifecycle (list, clear, sanitize).
type NamespaceManager struct {
	db *sql.DB
}

func NewNamespaceManager(db *sql.DB) *NamespaceManager {
	return &NamespaceManager{db: db}
}

// List returns all distinct namespaces across memory stores.
func (m *NamespaceManager) List() ([]string, error) {
	seen := make(map[string]bool)

	// From memory_chunks (source field)
	rows, err := m.db.Query(`SELECT DISTINCT source FROM memory_chunks WHERE source != '' ORDER BY source`)
	if err != nil {
		return nil, fmt.Errorf("list from memory_chunks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if s != "" {
			seen[SanitizeNamespace(s)] = true
		}
	}
	rows.Close()

	// From kv_namespace
	kvRows, err := m.db.Query(`SELECT DISTINCT namespace FROM kv_namespace ORDER BY namespace`)
	if err != nil {
		return nil, fmt.Errorf("list from kv_namespace: %w", err)
	}
	defer kvRows.Close()
	for kvRows.Next() {
		var s string
		if err := kvRows.Scan(&s); err != nil {
			return nil, err
		}
		if s != "" {
			seen[SanitizeNamespace(s)] = true
		}
	}
	kvRows.Close()

	result := make([]string, 0, len(seen))
	for ns := range seen {
		result = append(result, ns)
	}
	return result, nil
}

// Clear removes all data for a namespace across all memory stores.
func (m *NamespaceManager) Clear(ns string) error {
	ns = SanitizeNamespace(ns)
	// Escape LIKE wildcards so namespace names containing % or _ match literally.
	likeNs := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(ns)

	// Delete from memory_chunks
	if _, err := m.db.Exec(`DELETE FROM memory_chunks WHERE source = ?`, ns); err != nil {
		return fmt.Errorf("clear memory_chunks: %w", err)
	}
	// Delete from memory_conversations (by matching source in metadata, best effort)
	if _, err := m.db.Exec(`DELETE FROM memory_chunks WHERE source LIKE ? ESCAPE '\'`, likeNs+":%"); err != nil {
		return fmt.Errorf("clear memory_chunks subsources: %w", err)
	}
	// Delete from kv_namespace
	if _, err := m.db.Exec(`DELETE FROM kv_namespace WHERE namespace = ?`, ns); err != nil {
		return fmt.Errorf("clear kv_namespace: %w", err)
	}
	// Delete from tool_rules where the namespace appears as a JSON string value
	// in tags_json. Using the quoted form avoids matching substrings of other values
	// (e.g. ns="go" won't match tags='["golang"]').
	if _, err := m.db.Exec(`DELETE FROM tool_rules WHERE tags_json LIKE ? ESCAPE '\'`, `%"`+likeNs+`"%`); err != nil {
		return fmt.Errorf("clear tool_rules: %w", err)
	}
	// Delete from mem_tree_nodes (source-scoped).
	if _, err := m.db.Exec(`DELETE FROM mem_tree_nodes WHERE id LIKE ? ESCAPE '\'`, likeNs+":%"); err != nil {
		return fmt.Errorf("clear mem_tree_nodes: %w", err)
	}

	return nil
}

// GLOBAL_NAMESPACE is the default namespace for unscoped data.
const GLOBAL_NAMESPACE = "__global__"

// SanitizeNamespace returns a safe, collision-free encoding of a namespace.
// Uses percent-encoding for special characters to prevent distinct namespaces
// (e.g. "a.b" and "a_b") from silently colliding.
func SanitizeNamespace(ns string) string {
	if ns == "" {
		return "default"
	}
	needsEncoding := false
	for _, r := range ns {
		if r == '/' || r == '\\' || r == '.' || r == ':' ||
			r == '*' || r == '?' || r == '"' || r == '<' ||
			r == '>' || r == '|' || r == ' ' {
			needsEncoding = true
			break
		}
	}
	if !needsEncoding {
		return ns
	}
	// Percent-encode special characters as _XX_ where XX is the hex byte value.
	var b strings.Builder
	for _, r := range ns {
		switch r {
		case '/':
			b.WriteString("_2f_")
		case '\\':
			b.WriteString("_5c_")
		case '.':
			b.WriteString("_2e_")
		case ' ':
			b.WriteString("_20_")
		case ':':
			b.WriteString("_3a_")
		case '*':
			b.WriteString("_2a_")
		case '?':
			b.WriteString("_3f_")
		case '"':
			b.WriteString("_22_")
		case '<':
			b.WriteString("_3c_")
		case '>':
			b.WriteString("_3e_")
		case '|':
			b.WriteString("_7c_")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
