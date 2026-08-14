package store

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/simon/mneme/internal/sqlite"
)

func TestStore_InsertAndSearch(t *testing.T) {
	db := openDB(t)
	s, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	s.Insert(MemoryChunk{Source: "conversation", Content: "User asked about blockchain scalability solutions", Summary: "blockchain scalability"})
	s.Insert(MemoryChunk{Source: "conversation", Content: "Discussed weather and weekend plans", Summary: "casual chat"})

	results, err := s.Search("blockchain", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 1 {
		t.Error("expected blockchain search results")
	}
	if len(results) > 0 && results[0].Source != "conversation" {
		t.Errorf("expected conversation source, got %s", results[0].Source)
	}
}

func TestStore_ListRecent(t *testing.T) {
	db := openDB(t)
	s, _ := NewStore(db)
	s.Insert(MemoryChunk{Source: "test", Content: "first"})
	s.Insert(MemoryChunk{Source: "test", Content: "second"})

	results, err := s.ListRecent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 {
		t.Errorf("expected at least 2 results, got %d", len(results))
	}
}

func openDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestEscapeFTS5Query(t *testing.T) {
	tests := []struct {
		input    string
		contains string
		not      string
	}{
		{"hello world", `"hello" "world"`, "OR"},
		{"test", `"test"`, ""},
		{"", `""`, ""},
		{"foo* bar)", `"foo" "bar"`, "*"},
	}
	for _, tt := range tests {
		result := escapeFTS5Query(tt.input)
		if tt.contains != "" && !strings.Contains(result, tt.contains) {
			t.Errorf("escapeFTS5Query(%q): expected to contain %q, got %q", tt.input, tt.contains, result)
		}
		if tt.not != "" && strings.Contains(result, tt.not) {
			t.Errorf("escapeFTS5Query(%q): should NOT contain %q, got %q", tt.input, tt.not, result)
		}
	}
	// Verify max 8 tokens.
	long := "a b c d e f g h i j k l"
	result := escapeFTS5Query(long)
	if strings.Count(result, "\"") > 16 {
		t.Errorf("expected at most 8 tokens, got %q", result)
	}
}
