package migration

import (
	"database/sql"
	"testing"

	_ "github.com/simon/mneme/internal/sqlite"
)

var v1 = Migration{
	Version: 1,
	Name:    "initial schema",
	Up: func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`)
		return err
	},
}

func TestRunner_AppliesMigrationsInOrder(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	runner := NewRunner(db)
	runner.Register(v1)

	if err := runner.Up(); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT count(*) FROM migration_versions WHERE version = 1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected version 1 recorded, got %d", count)
	}

	if _, err := db.Exec("INSERT INTO config (key, value) VALUES ('test', 'val')"); err != nil {
		t.Errorf("config table not functional: %v", err)
	}
}

func TestRunner_SkipsAppliedMigrations(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	runner := NewRunner(db)
	runner.Register(v1)

	if err := runner.Up(); err != nil {
		t.Fatal(err)
	}

	if err := runner.Up(); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
}
