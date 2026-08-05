package migration

import (
	"database/sql"
	"fmt"
	"sort"
)

type Migration struct {
	Version int
	Name    string
	Up      func(tx *sql.Tx) error
}

type Runner struct {
	db         *sql.DB
	migrations map[int]Migration
}

func NewRunner(db *sql.DB) *Runner {
	return &Runner{
		db:         db,
		migrations: make(map[int]Migration),
	}
}

func (r *Runner) Register(m Migration) {
	r.migrations[m.Version] = m
}

func (r *Runner) Up() error {
	if _, err := r.db.Exec(`CREATE TABLE IF NOT EXISTS migration_versions (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("create migration_versions: %w", err)
	}

	versions := make([]int, 0, len(r.migrations))
	for v := range r.migrations {
		versions = append(versions, v)
	}
	sort.Ints(versions)

	for _, v := range versions {
		m := r.migrations[v]
		if r.isApplied(v) {
			continue
		}
		tx, err := r.db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for v%d: %w", v, err)
		}
		defer tx.Rollback() // no-op after Commit, safe on all paths
		if err := m.Up(tx); err != nil {
			return fmt.Errorf("migration v%d (%s): %w", v, m.Name, err)
		}
		if _, err := tx.Exec("INSERT INTO migration_versions (version, name) VALUES (?, ?)", v, m.Name); err != nil {
			return fmt.Errorf("record v%d: %w", v, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit v%d: %w", v, err)
		}
	}
	return nil
}

func (r *Runner) isApplied(version int) bool {
	var count int
	if err := r.db.QueryRow("SELECT count(*) FROM migration_versions WHERE version = ?", version).Scan(&count); err != nil {
		return false // table may not exist yet
	}
	return count > 0
}
