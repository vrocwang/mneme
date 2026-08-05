package notifications

import (
	"database/sql"
	"log/slog"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestNewBus(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer db.Close()

	bus, err := NewBus(db, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	if bus == nil {
		t.Fatal("expected non-nil bus")
	}
}

func TestBusNilDB(t *testing.T) {
	_, err := NewBus(nil, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err == nil {
		t.Error("expected error for nil db")
	}
}
