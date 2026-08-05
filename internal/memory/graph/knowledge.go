package graph

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Triple is a knowledge graph edge: subject → predicate → object.
type Triple struct {
	Subject   string            `json:"subject"`
	Predicate string            `json:"predicate"`
	Object    string            `json:"object"`
	Namespace string            `json:"namespace,omitempty"`
	Attrs     map[string]string `json:"attrs,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// KnowledgeGraph stores typed relations (S-P-O triples) with SQLite persistence.
type KnowledgeGraph struct {
	mu sync.RWMutex
	db *sql.DB
}

// NewKnowledgeGraph creates a knowledge graph backed by SQLite.
func NewKnowledgeGraph(db *sql.DB) (*KnowledgeGraph, error) {
	kg := &KnowledgeGraph{db: db}
	if err := kg.migrate(); err != nil {
		return nil, fmt.Errorf("knowledge graph migration: %w", err)
	}
	return kg, nil
}

func (kg *KnowledgeGraph) migrate() error {
	_, err := kg.db.Exec(`
		CREATE TABLE IF NOT EXISTS kg_triples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subject TEXT NOT NULL,
			predicate TEXT NOT NULL,
			object TEXT NOT NULL,
			namespace TEXT NOT NULL DEFAULT 'default',
			attrs TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_kg_subject ON kg_triples(subject, namespace);
		CREATE INDEX IF NOT EXISTS idx_kg_object ON kg_triples(object, namespace);
		CREATE INDEX IF NOT EXISTS idx_kg_predicate ON kg_triples(predicate);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_kg_triple_unique ON kg_triples(subject, predicate, object, namespace);
	`)
	return err
}

// Upsert adds or updates a knowledge graph triple.
func (kg *KnowledgeGraph) Upsert(subject, predicate, object, namespace string, attrs map[string]string) error {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	if namespace == "" {
		namespace = "default"
	}

	attrsJSON := "{}"
	if len(attrs) > 0 {
		parts := make([]string, 0, len(attrs))
		for k, v := range attrs {
			parts = append(parts, fmt.Sprintf("%q:%q", k, v))
		}
		attrsJSON = "{" + strings.Join(parts, ",") + "}"
	}

	_, err := kg.db.Exec(
		`INSERT INTO kg_triples (subject, predicate, object, namespace, attrs) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(subject, predicate, object, namespace) DO UPDATE SET attrs = excluded.attrs`,
		subject, predicate, object, namespace, attrsJSON,
	)
	return err
}

// Query returns triples matching the given filters.
func (kg *KnowledgeGraph) Query(subject, predicate, namespace string, limit int) ([]Triple, error) {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	var rows *sql.Rows
	var err error

	switch {
	case subject != "" && predicate != "":
		rows, err = kg.db.Query(
			`SELECT subject, predicate, object, namespace, attrs, created_at FROM kg_triples
			 WHERE subject = ? AND predicate = ? AND namespace = ? LIMIT ?`,
			subject, predicate, namespace, limit,
		)
	case subject != "":
		rows, err = kg.db.Query(
			`SELECT subject, predicate, object, namespace, attrs, created_at FROM kg_triples
			 WHERE subject = ? AND namespace = ? LIMIT ?`,
			subject, namespace, limit,
		)
	case predicate != "":
		rows, err = kg.db.Query(
			`SELECT subject, predicate, object, namespace, attrs, created_at FROM kg_triples
			 WHERE predicate = ? AND namespace = ? LIMIT ?`,
			predicate, namespace, limit,
		)
	default:
		rows, err = kg.db.Query(
			`SELECT subject, predicate, object, namespace, attrs, created_at FROM kg_triples
			 WHERE namespace = ? LIMIT ?`,
			namespace, limit,
		)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var triples []Triple
	for rows.Next() {
		var t Triple
		var attrsStr string
		if err := rows.Scan(&t.Subject, &t.Predicate, &t.Object, &t.Namespace, &attrsStr, &t.CreatedAt); err != nil {
			continue
		}
		// Simple JSON parsing for flat string map
		t.Attrs = parseSimpleAttrs(attrsStr)
		triples = append(triples, t)
	}
	return triples, rows.Err()
}

// QueryByObject returns triples where the object matches.
func (kg *KnowledgeGraph) QueryByObject(object, namespace string, limit int) ([]Triple, error) {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}
	if namespace == "" {
		namespace = "default"
	}

	rows, err := kg.db.Query(
		`SELECT subject, predicate, object, namespace, attrs, created_at FROM kg_triples
		 WHERE object = ? AND namespace = ? LIMIT ?`,
		object, namespace, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var triples []Triple
	for rows.Next() {
		var t Triple
		var attrsStr string
		if err := rows.Scan(&t.Subject, &t.Predicate, &t.Object, &t.Namespace, &attrsStr, &t.CreatedAt); err != nil {
			continue
		}
		t.Attrs = parseSimpleAttrs(attrsStr)
		triples = append(triples, t)
	}
	return triples, rows.Err()
}

// ListPredicates returns all distinct predicates in a namespace.
func (kg *KnowledgeGraph) ListPredicates(namespace string) ([]string, error) {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	if namespace == "" {
		namespace = "default"
	}

	rows, err := kg.db.Query(`SELECT DISTINCT predicate FROM kg_triples WHERE namespace = ? ORDER BY predicate`, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var predicates []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			continue
		}
		predicates = append(predicates, p)
	}
	return predicates, rows.Err()
}

// Delete removes a triple.
func (kg *KnowledgeGraph) Delete(subject, predicate, object, namespace string) error {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	if namespace == "" {
		namespace = "default"
	}

	_, err := kg.db.Exec(
		`DELETE FROM kg_triples WHERE subject = ? AND predicate = ? AND object = ? AND namespace = ?`,
		subject, predicate, object, namespace,
	)
	return err
}

// DeleteBySubject removes all triples for a subject.
func (kg *KnowledgeGraph) DeleteBySubject(subject, namespace string) (int64, error) {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	if namespace == "" {
		namespace = "default"
	}

	result, err := kg.db.Exec(
		`DELETE FROM kg_triples WHERE subject = ? AND namespace = ?`,
		subject, namespace,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// Stats returns counts.
func (kg *KnowledgeGraph) Stats(namespace string) map[string]int64 {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	if namespace == "" {
		namespace = "default"
	}

	var total, subjects, predicates int64
	kg.db.QueryRow(`SELECT COUNT(*) FROM kg_triples WHERE namespace = ?`, namespace).Scan(&total)
	kg.db.QueryRow(`SELECT COUNT(DISTINCT subject) FROM kg_triples WHERE namespace = ?`, namespace).Scan(&subjects)
	kg.db.QueryRow(`SELECT COUNT(DISTINCT predicate) FROM kg_triples WHERE namespace = ?`, namespace).Scan(&predicates)
	return map[string]int64{
		"total":      total,
		"subjects":   subjects,
		"predicates": predicates,
	}
}

// FormatTriples returns a human-readable representation.
func FormatTriples(triples []Triple) string {
	if len(triples) == 0 {
		return "No relations found."
	}
	// Group by subject.
	bySubject := make(map[string][]Triple)
	for _, t := range triples {
		bySubject[t.Subject] = append(bySubject[t.Subject], t)
	}

	subjects := make([]string, 0, len(bySubject))
	for s := range bySubject {
		subjects = append(subjects, s)
	}
	sort.Strings(subjects)

	var b strings.Builder
	for _, subject := range subjects {
		b.WriteString(fmt.Sprintf("## %s\n", subject))
		for _, t := range bySubject[subject] {
			b.WriteString(fmt.Sprintf("- %s → %s: %s\n", t.Predicate, t.Object, formatAttrs(t.Attrs)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatAttrs(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(attrs))
	for k, v := range attrs {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// parseSimpleAttrs parses our simple key:value JSON format.
func parseSimpleAttrs(s string) map[string]string {
	if s == "" || s == "{}" {
		return nil
	}
	// Use the standard JSON decoder for correct handling of
	// commas-in-values, escape sequences, and nested quoting.
	var attrs map[string]string
	if err := json.Unmarshal([]byte(s), &attrs); err != nil {
		return nil
	}
	return attrs
}
