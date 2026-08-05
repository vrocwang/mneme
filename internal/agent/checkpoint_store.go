package agent

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// SQLiteCheckPointStore implements eino's CheckPointStore interface backed by
// SQLite. It provides persistent storage for agent execution checkpoints,
// enabling long-running tasks to be paused and resumed across process restarts.
type SQLiteCheckPointStore struct {
	db *sql.DB
	mu sync.Mutex
}

// NewSQLiteCheckPointStore creates the store and runs the schema migration.
func NewSQLiteCheckPointStore(db *sql.DB) (*SQLiteCheckPointStore, error) {
	s := &SQLiteCheckPointStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("checkpoint_store: migrate: %w", err)
	}
	return s, nil
}

func (s *SQLiteCheckPointStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_checkpoints (
			id TEXT PRIMARY KEY,
			data BLOB NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_agent_checkpoints_updated
			ON agent_checkpoints(updated_at);
	`)
	return err
}

// Get retrieves a checkpoint by ID. Returns (nil, false, nil) when the
// checkpoint does not exist.
func (s *SQLiteCheckPointStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var data []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT data FROM agent_checkpoints WHERE id = ?`,
		checkPointID,
	).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("checkpoint_store: get %q: %w", checkPointID, err)
	}
	return data, true, nil
}

// Set persists a checkpoint. If a checkpoint with the same ID already exists,
// it is overwritten while preserving the original created_at timestamp.
func (s *SQLiteCheckPointStore) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)

	// Try to preserve the original created_at if this is an update.
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT created_at FROM agent_checkpoints WHERE id = ?`,
		checkPointID,
	).Scan(&createdAt)
	if err != nil {
		createdAt = now // new record
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO agent_checkpoints (id, data, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		checkPointID, checkPoint, createdAt, now,
	)
	if err != nil {
		return fmt.Errorf("checkpoint_store: set %q: %w", checkPointID, err)
	}
	return nil
}
