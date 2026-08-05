package dag

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Store persists DAG graphs and run records to SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a DAG Store backed by the given database connection.
// The dag_graphs and dag_runs tables are created if they don't exist.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("dag store: db is nil")
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS dag_graphs (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			graph_json  TEXT NOT NULL,
			created_at  TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`); err != nil {
		return nil, fmt.Errorf("dag store: create dag_graphs table: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS dag_runs (
			id          TEXT PRIMARY KEY,
			graph_name  TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'pending',
			input_json  TEXT,
			output_json TEXT,
			error       TEXT,
			steps_json  TEXT,
			step_index  INTEGER DEFAULT 0,
			started_at  TEXT NOT NULL,
			ended_at    TEXT
		)
	`); err != nil {
		return nil, fmt.Errorf("dag store: create dag_runs table: %w", err)
	}

	return &Store{db: db}, nil
}

// SaveGraph persists a graph definition.
func (s *Store) SaveGraph(graph *Graph) error {
	if err := graph.Validate(); err != nil {
		return err
	}

	data, err := graph.ToJSON()
	if err != nil {
		return fmt.Errorf("dag store: marshal graph: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO dag_graphs (id, name, graph_json, updated_at)
		 VALUES (?, ?, ?, datetime('now'))`,
		graph.Name, graph.Name, string(data),
	)
	return err
}

// GetGraph loads a graph definition by name.
func (s *Store) GetGraph(name string) (*Graph, error) {
	var graphJSON string
	err := s.db.QueryRow(
		`SELECT graph_json FROM dag_graphs WHERE id = ?`, name,
	).Scan(&graphJSON)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("dag store: graph %q not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("dag store: query graph: %w", err)
	}

	return GraphFromJSON([]byte(graphJSON))
}

// ListGraphs returns all saved graph names.
func (s *Store) ListGraphs() ([]string, error) {
	rows, err := s.db.Query(`SELECT name FROM dag_graphs ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("dag store: list graphs: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("dag store: scan graph name: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// DeleteGraph removes a graph definition.
func (s *Store) DeleteGraph(name string) error {
	_, err := s.db.Exec(`DELETE FROM dag_graphs WHERE id = ?`, name)
	return err
}

// SaveRun persists a run record.
func (s *Store) SaveRun(rec *RunRecord) error {
	stepsJSON, err := json.Marshal(rec.Steps)
	if err != nil {
		stepsJSON = []byte("[]")
	}
	inputJSON, err := json.Marshal(rec.Input)
	if err != nil {
		inputJSON = []byte("null")
	}
	outputJSON, err := json.Marshal(rec.Output)
	if err != nil {
		outputJSON = []byte("null")
	}
	endedAt := ""
	if !rec.EndedAt.IsZero() {
		endedAt = rec.EndedAt.Format(time.RFC3339)
	}

	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO dag_runs
		 (id, graph_name, status, input_json, output_json, error, steps_json, step_index, started_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.GraphName, string(rec.Status),
		string(inputJSON), string(outputJSON), rec.Error,
		string(stepsJSON), rec.StepIndex,
		rec.StartedAt.Format(time.RFC3339), endedAt,
	)
	return err
}

// GetRun loads a run record by ID.
func (s *Store) GetRun(id string) (*RunRecord, error) {
	var (
		status                                   string
		inputJSON, outputJSON, stepsJSON, errMsg sql.NullString
		startedAt, endedAt                       string
		stepIndex                                int
		rec                                      RunRecord
	)
	err := s.db.QueryRow(
		`SELECT id, graph_name, status, input_json, output_json, error, steps_json, step_index, started_at, ended_at
		 FROM dag_runs WHERE id = ?`, id,
	).Scan(&rec.ID, &rec.GraphName, &status, &inputJSON, &outputJSON, &errMsg, &stepsJSON, &stepIndex, &startedAt, &endedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("dag store: run %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("dag store: query run: %w", err)
	}

	rec.Status = RunStatus(status)
	rec.StepIndex = stepIndex
	rec.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	if endedAt != "" {
		rec.EndedAt, _ = time.Parse(time.RFC3339, endedAt)
	}
	if errMsg.Valid {
		rec.Error = errMsg.String
	}
	if inputJSON.Valid {
		json.Unmarshal([]byte(inputJSON.String), &rec.Input)
	}
	if outputJSON.Valid {
		json.Unmarshal([]byte(outputJSON.String), &rec.Output)
	}
	if stepsJSON.Valid {
		json.Unmarshal([]byte(stepsJSON.String), &rec.Steps)
	}

	return &rec, nil
}

// ListRuns returns recent run records, newest first.
func (s *Store) ListRuns(limit int) ([]*RunRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT id, graph_name, status, error, started_at, ended_at
		 FROM dag_runs ORDER BY started_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("dag store: list runs: %w", err)
	}
	defer rows.Close()

	var runs []*RunRecord
	for rows.Next() {
		var rec RunRecord
		var status, startedAt, endedAt string
		var errMsg sql.NullString
		if err := rows.Scan(&rec.ID, &rec.GraphName, &status, &errMsg, &startedAt, &endedAt); err != nil {
			return nil, fmt.Errorf("dag store: scan run: %w", err)
		}
		rec.Status = RunStatus(status)
		rec.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
		if endedAt != "" {
			rec.EndedAt, _ = time.Parse(time.RFC3339, endedAt)
		}
		if errMsg.Valid {
			rec.Error = errMsg.String
		}
		runs = append(runs, &rec)
	}
	return runs, rows.Err()
}
