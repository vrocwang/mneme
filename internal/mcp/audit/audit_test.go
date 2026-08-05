package audit

import (
	"testing"
)

func TestLog_RecordAndList(t *testing.T) {
	l := New(100)
	l.Record(Entry{Server: "filesystem", Tool: "read_file", WriteOp: false})
	l.Record(Entry{Server: "filesystem", Tool: "write_file", WriteOp: true})

	entries := l.List(10)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Newest first
	if entries[0].Tool != "write_file" {
		t.Errorf("expected write_file first (newest), got %s", entries[0].Tool)
	}
}

func TestLog_RecordToolCall(t *testing.T) {
	l := New(100)
	finalize := l.RecordToolCall("github", "create_issue", map[string]interface{}{"title": "bug"}, true)
	finalize("Issue #42 created", "")

	entries := l.List(1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Server != "github" {
		t.Errorf("expected github server, got %s", entries[0].Server)
	}
	if entries[0].Result != "Issue #42 created" {
		t.Errorf("unexpected result: %s", entries[0].Result)
	}
}

func TestLog_RecordToolCall_Error(t *testing.T) {
	l := New(100)
	finalize := l.RecordToolCall("shell", "rm", nil, true)
	finalize("", "permission denied")

	entries := l.List(1)
	if entries[0].Error != "permission denied" {
		t.Errorf("expected error, got %q", entries[0].Error)
	}
	if !entries[0].WriteOp {
		t.Error("shell should be a write op")
	}
}

func TestLog_ListByServer(t *testing.T) {
	l := New(100)
	l.Record(Entry{Server: "github", Tool: "read_file"})
	l.Record(Entry{Server: "slack", Tool: "send_message", WriteOp: true})
	l.Record(Entry{Server: "github", Tool: "create_pr", WriteOp: true})

	githubEntries := l.ListByServer("github", 10)
	if len(githubEntries) != 2 {
		t.Errorf("expected 2 github entries, got %d", len(githubEntries))
	}
}

func TestLog_ListWriteOps(t *testing.T) {
	l := New(100)
	l.Record(Entry{Server: "fs", Tool: "read_file", WriteOp: false})
	l.Record(Entry{Server: "fs", Tool: "write_file", WriteOp: true})
	l.Record(Entry{Server: "shell", Tool: "npm_install", WriteOp: true})

	writeOps := l.ListWriteOps(10)
	if len(writeOps) != 2 {
		t.Errorf("expected 2 write ops, got %d", len(writeOps))
	}
}

func TestLog_Stats(t *testing.T) {
	l := New(100)
	l.Record(Entry{Server: "fs", Tool: "read_file", WriteOp: false})
	l.Record(Entry{Server: "fs", Tool: "write_file", WriteOp: true})
	l.Record(Entry{Server: "shell", Tool: "npm_install", WriteOp: true, Error: "failed"})

	stats := l.Stats()
	if stats["total"].(int) != 3 {
		t.Errorf("expected 3 total, got %v", stats["total"])
	}
	if stats["write_ops"].(int) != 2 {
		t.Errorf("expected 2 write_ops, got %v", stats["write_ops"])
	}
	if stats["errors"].(int) != 1 {
		t.Errorf("expected 1 error, got %v", stats["errors"])
	}
}

func TestLog_PrunesOldest(t *testing.T) {
	l := New(5)
	for i := 0; i < 10; i++ {
		l.Record(Entry{Server: "test", Tool: "test"})
	}
	entries := l.List(0)
	if len(entries) > 5 {
		t.Errorf("expected max 5, got %d", len(entries))
	}
}

func TestIsWriteTool(t *testing.T) {
	if !IsWriteTool("write_file") {
		t.Error("write_file should be a write tool")
	}
	if !IsWriteTool("shell") {
		t.Error("shell should be a write tool")
	}
	if IsWriteTool("read_file") {
		t.Error("read_file should not be a write tool")
	}
}

func TestFormatEntries(t *testing.T) {
	l := New(10)
	l.Record(Entry{Server: "fs", Tool: "read_file", WriteOp: false, Duration: "5ms"})
	l.Record(Entry{Server: "shell", Tool: "npm_install", WriteOp: true, Duration: "2s"})

	output := FormatEntries(l.List(10))
	if output == "" {
		t.Error("expected non-empty output")
	}
}

func TestFormatEntries_Empty(t *testing.T) {
	output := FormatEntries(nil)
	if output == "" {
		t.Error("expected output for empty entries")
	}
}
