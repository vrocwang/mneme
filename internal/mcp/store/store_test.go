package store

import (
	"database/sql"
	"testing"

	"github.com/simon/mneme/internal/capability"
	_ "github.com/simon/mneme/internal/sqlite"
)

// newTestStore creates an MCP store backed by an in-memory SQLite database.
// A single connection is forced so that ":memory:" is shared across queries.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	s, err := NewStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s
}

// TestStore_PersistMCPServer verifies that PersistMCPServer writes a record
// that is fully retrievable via GetServer, exercising the field mapping from
// capability.PersistedMCPServer to InstalledServer.
func TestStore_PersistMCPServer(t *testing.T) {
	s := newTestStore(t)
	srv := &capability.PersistedMCPServer{
		ServerID:      "mcp:github",
		QualifiedName: "github",
		DisplayName:   "GitHub",
		Command:       "npx",
		Args:          []string{"-y", "@modelcontextprotocol/server-github"},
		Transport:     "stdio",
		DeploymentURL: "https://example.com",
		Enabled:       true,
	}
	if err := s.PersistMCPServer(srv); err != nil {
		t.Fatalf("PersistMCPServer: %v", err)
	}

	got, err := s.GetServer("mcp:github")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got.ServerID != "mcp:github" {
		t.Errorf("ServerID = %q, want mcp:github", got.ServerID)
	}
	if got.Command != "npx" {
		t.Errorf("Command = %q, want npx", got.Command)
	}
	if len(got.Args) != 2 || got.Args[0] != "-y" {
		t.Errorf("Args = %v, want [-y @modelcontextprotocol/server-github]", got.Args)
	}
	if got.Transport != "stdio" {
		t.Errorf("Transport = %q, want stdio", got.Transport)
	}
	if !got.Enabled {
		t.Error("Enabled = false, want true")
	}
	if got.DeploymentURL != "https://example.com" {
		t.Errorf("DeploymentURL = %q, want https://example.com", got.DeploymentURL)
	}
}

// TestStore_PersistMCPServer_IdempotentUpsert verifies that re-persisting a
// server with the same ID replaces the row instead of duplicating it.
func TestStore_PersistMCPServer_IdempotentUpsert(t *testing.T) {
	s := newTestStore(t)
	srv := &capability.PersistedMCPServer{
		ServerID: "mcp:fs", QualifiedName: "fs", DisplayName: "FS",
		Command: "npx", Args: []string{"fs-server"}, Transport: "stdio", Enabled: true,
	}
	if err := s.PersistMCPServer(srv); err != nil {
		t.Fatalf("first persist: %v", err)
	}
	srv.DisplayName = "Filesystem"
	if err := s.PersistMCPServer(srv); err != nil {
		t.Fatalf("second persist: %v", err)
	}
	list, err := s.ListServers()
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 server after upsert, got %d", len(list))
	}
	if list[0].DisplayName != "Filesystem" {
		t.Errorf("DisplayName = %q, want Filesystem", list[0].DisplayName)
	}
}

// TestStore_RemoveMCPServer verifies that RemoveMCPServer deletes the
// persisted record and is safe to call on a missing ID.
func TestStore_RemoveMCPServer(t *testing.T) {
	s := newTestStore(t)
	srv := &capability.PersistedMCPServer{
		ServerID: "mcp:tmp", QualifiedName: "tmp", DisplayName: "Temp",
		Command: "npx", Transport: "stdio", Enabled: true,
	}
	if err := s.PersistMCPServer(srv); err != nil {
		t.Fatalf("PersistMCPServer: %v", err)
	}
	if err := s.RemoveMCPServer("mcp:tmp"); err != nil {
		t.Fatalf("RemoveMCPServer: %v", err)
	}
	list, err := s.ListServers()
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 servers after removal, got %d", len(list))
	}
	if err := s.RemoveMCPServer("mcp:nonexistent"); err != nil {
		t.Errorf("RemoveMCPServer on missing id should not error, got: %v", err)
	}
}
