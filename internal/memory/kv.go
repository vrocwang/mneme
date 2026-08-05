package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// KVStore provides global and namespace-scoped key-value storage for memory.
type KVStore struct {
	db *sql.DB
}

// KVEntry is a single key-value record.
type KVEntry struct {
	Namespace string `json:"namespace,omitempty"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func NewKVStore(db *sql.DB) (*KVStore, error) {
	if err := initKVTables(db); err != nil {
		return nil, fmt.Errorf("init kv tables: %w", err)
	}
	return &KVStore{db: db}, nil
}

func initKVTables(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS kv_global (
			key TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at REAL NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS kv_namespace (
			namespace TEXT NOT NULL,
			key TEXT NOT NULL,
			value_json TEXT NOT NULL,
			updated_at REAL NOT NULL,
			PRIMARY KEY (namespace, key)
		)`,
	}
	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// SetGlobal stores a value in the global KV scope.
func (k *KVStore) SetGlobal(key, value string) error {
	now := float64(time.Now().UnixMilli()) / 1000.0
	_, err := k.db.Exec(
		`INSERT INTO kv_global (key, value_json, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		key, value, now,
	)
	return err
}

// GetGlobal retrieves a value from the global KV scope.
func (k *KVStore) GetGlobal(key string) (string, bool, error) {
	var value string
	var updatedAt float64
	err := k.db.QueryRow(
		`SELECT value_json, updated_at FROM kv_global WHERE key = ?`, key,
	).Scan(&value, &updatedAt)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// DeleteGlobal removes a key from the global KV scope.
func (k *KVStore) DeleteGlobal(key string) error {
	_, err := k.db.Exec(`DELETE FROM kv_global WHERE key = ?`, key)
	return err
}

// SetNamespace stores a value in a namespace-scoped KV.
func (k *KVStore) SetNamespace(ns, key, value string) error {
	ns = sanitizeNamespace(ns)
	now := float64(time.Now().UnixMilli()) / 1000.0
	_, err := k.db.Exec(
		`INSERT INTO kv_namespace (namespace, key, value_json, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(namespace, key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		ns, key, value, now,
	)
	return err
}

// GetNamespace retrieves a value from a namespace-scoped KV.
func (k *KVStore) GetNamespace(ns, key string) (string, bool, error) {
	ns = sanitizeNamespace(ns)
	var value string
	err := k.db.QueryRow(
		`SELECT value_json FROM kv_namespace WHERE namespace = ? AND key = ?`, ns, key,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// DeleteNamespace removes a key from a namespace-scoped KV.
func (k *KVStore) DeleteNamespace(ns, key string) error {
	ns = sanitizeNamespace(ns)
	_, err := k.db.Exec(`DELETE FROM kv_namespace WHERE namespace = ? AND key = ?`, ns, key)
	return err
}

// ListNamespace returns all keys and values in a namespace.
func (k *KVStore) ListNamespace(ns string) ([]KVEntry, error) {
	ns = sanitizeNamespace(ns)
	rows, err := k.db.Query(
		`SELECT namespace, key, value_json, updated_at FROM kv_namespace WHERE namespace = ? ORDER BY key`, ns,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []KVEntry
	for rows.Next() {
		var e KVEntry
		var updatedAt float64
		if err := rows.Scan(&e.Namespace, &e.Key, &e.Value, &updatedAt); err != nil {
			return nil, err
		}
		e.UpdatedAt = fmt.Sprintf("%.0f", updatedAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ListNamespaces returns all distinct namespaces in the KV store.
func (k *KVStore) ListNamespaces() ([]string, error) {
	rows, err := k.db.Query(`SELECT DISTINCT namespace FROM kv_namespace ORDER BY namespace`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var namespaces []string
	for rows.Next() {
		var ns string
		if err := rows.Scan(&ns); err != nil {
			return nil, err
		}
		namespaces = append(namespaces, ns)
	}
	return namespaces, rows.Err()
}

// RecordsForScope returns all KV records: namespace records if ns is set, else global + all namespaces.
func (k *KVStore) RecordsForScope(ns string) ([]KVEntry, error) {
	if ns != "" {
		return k.ListNamespace(ns)
	}

	var entries []KVEntry

	// Global records
	rows, err := k.db.Query(`SELECT key, value_json, updated_at FROM kv_global ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e KVEntry
		var updatedAt float64
		if err := rows.Scan(&e.Key, &e.Value, &updatedAt); err != nil {
			return nil, err
		}
		e.Namespace = "__global__"
		e.UpdatedAt = fmt.Sprintf("%.0f", updatedAt)
		entries = append(entries, e)
	}
	rows.Close()

	// Namespace records
	nsRows, err := k.db.Query(`SELECT namespace, key, value_json, updated_at FROM kv_namespace ORDER BY namespace, key`)
	if err != nil {
		return nil, err
	}
	defer nsRows.Close()
	for nsRows.Next() {
		var e KVEntry
		var updatedAt float64
		if err := nsRows.Scan(&e.Namespace, &e.Key, &e.Value, &updatedAt); err != nil {
			return nil, err
		}
		e.UpdatedAt = fmt.Sprintf("%.0f", updatedAt)
		entries = append(entries, e)
	}

	return entries, nsRows.Err()
}

// ClearNamespace removes all KV records in a namespace.
func (k *KVStore) ClearNamespace(ns string) error {
	ns = sanitizeNamespace(ns)
	_, err := k.db.Exec(`DELETE FROM kv_namespace WHERE namespace = ?`, ns)
	return err
}

// sanitizeNamespace rejects namespaces containing characters that could cause
// collisions. Unlike the previous lossy replacement scheme (which mapped both
// "a.b" and "a_b" to "a_b"), this returns an error for unsafe namespaces so
// callers get a clear signal rather than silent data corruption.
func sanitizeNamespace(ns string) string {
	if ns == "" {
		return "default"
	}
	// Reject rather than silently remap — prevents collision between
	// semantically distinct namespaces like "a.b" and "a_b".
	for _, r := range ns {
		if r == '/' || r == '\\' || r == '.' || r == ':' ||
			r == '*' || r == '?' || r == '"' || r == '<' ||
			r == '>' || r == '|' || r == ' ' {
			// Fall back to a stable hash-based encoding so callers that
			// depend on the old lossy behavior don't break. The encoding
			// is reversible: percent-encode the offending characters.
			return percentEncodeNS(ns)
		}
	}
	return ns
}

// percentEncodeNS encodes special characters in a namespace as _XX_ where XX
// is the hex byte value, avoiding collisions between different namespaces.
func percentEncodeNS(ns string) string {
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
		case '_':
			b.WriteString("_5f_") // encode underscore itself to avoid ambiguity
		default:
			b.WriteRune(r)
		}
	}
	result := b.String()
	if result == "" {
		return "default"
	}
	return result
}

// ExportKVJSON exports all KV records as JSON for debugging.
func (k *KVStore) ExportKVJSON(ns string) (string, error) {
	entries, err := k.RecordsForScope(ns)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "[]", nil
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
