package agent

import (
	"database/sql"
	"testing"

	_ "github.com/simon/mneme/internal/sqlite"
)

// TestDefaultTaskMaxRetries_GoFallback verifies the in-memory fallback uses the
// shared constant (SET 1.3).
func TestDefaultTaskMaxRetries_GoFallback(t *testing.T) {
	if DefaultTaskMaxRetries != 3 {
		t.Fatalf("DefaultTaskMaxRetries = %d, want 3", DefaultTaskMaxRetries)
	}

	d := NewTaskDispatcher(nil, nil)

	unspecified := &DispatchTask{ID: "unspecified"}
	if err := d.Enqueue(unspecified); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if unspecified.MaxRetries != DefaultTaskMaxRetries {
		t.Errorf("MaxRetries = %d, want the default %d", unspecified.MaxRetries, DefaultTaskMaxRetries)
	}

	explicit := &DispatchTask{ID: "explicit", MaxRetries: 7}
	if err := d.Enqueue(explicit); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if explicit.MaxRetries != 7 {
		t.Errorf("MaxRetries = %d, want the caller value 7", explicit.MaxRetries)
	}
}

// TestDefaultTaskMaxRetries_DDLDefault proves the dispatch_tasks DDL default
// and the Go fallback are the same number: a row inserted without max_retries
// reads back as DefaultTaskMaxRetries.
func TestDefaultTaskMaxRetries_DDLDefault(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store, err := NewTaskStore(db)
	if err != nil {
		t.Fatalf("NewTaskStore: %v", err)
	}
	if store == nil {
		t.Fatal("NewTaskStore returned nil for a non-nil DB")
	}

	// Insert straight through SQL so the table DEFAULT is what is exercised —
	// TaskStore.Enqueue always supplies the column explicitly.
	if _, err := db.Exec(`INSERT INTO dispatch_tasks (id) VALUES (?)`, "raw"); err != nil {
		t.Fatalf("raw insert: %v", err)
	}

	task, err := store.Get("raw")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if task.MaxRetries != DefaultTaskMaxRetries {
		t.Errorf("DDL default max_retries = %d, want DefaultTaskMaxRetries (%d)",
			task.MaxRetries, DefaultTaskMaxRetries)
	}
}
