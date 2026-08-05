package sources

import (
	"testing"
	"time"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	s := &Source{
		ID:        "src-1",
		Name:      "My Project",
		Kind:      KindFolder,
		Target:    "/home/user/projects/myproject",
		Enabled:   true,
		AutoSync:  true,
		SyncEvery: "hourly",
	}
	r.Register(s)

	got := r.Get("src-1")
	if got == nil {
		t.Fatal("expected source, got nil")
	}
	if got.Kind != KindFolder {
		t.Errorf("expected folder kind, got %s", got.Kind)
	}
}

func TestRegistry_Remove(t *testing.T) {
	r := NewRegistry()
	r.Register(&Source{ID: "src-1", Name: "Test"})
	r.Remove("src-1")
	if r.Get("src-1") != nil {
		t.Error("expected nil after remove")
	}
}

func TestRegistry_ListByKind(t *testing.T) {
	r := NewRegistry()
	r.Register(&Source{ID: "1", Name: "Repo", Kind: KindGitHub, Enabled: true})
	r.Register(&Source{ID: "2", Name: "Feed", Kind: KindRSS, Enabled: true})
	r.Register(&Source{ID: "3", Name: "Docs", Kind: KindFolder, Enabled: true})

	folders := r.ListByKind(KindFolder)
	if len(folders) != 1 {
		t.Errorf("expected 1 folder, got %d", len(folders))
	}
	if folders[0].Name != "Docs" {
		t.Errorf("expected Docs, got %s", folders[0].Name)
	}
}

func TestRegistry_ListEnabled(t *testing.T) {
	r := NewRegistry()
	r.Register(&Source{ID: "1", Name: "A", Enabled: true})
	r.Register(&Source{ID: "2", Name: "B", Enabled: false})
	r.Register(&Source{ID: "3", Name: "C", Enabled: true})

	enabled := r.ListEnabled()
	if len(enabled) != 2 {
		t.Errorf("expected 2 enabled, got %d", len(enabled))
	}
}

func TestRegistry_ListAll(t *testing.T) {
	r := NewRegistry()
	r.Register(&Source{ID: "c", Name: "Charlie"})
	r.Register(&Source{ID: "a", Name: "Alice"})
	r.Register(&Source{ID: "b", Name: "Bob"})

	all := r.List()
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	// Sorted by name
	if all[0].Name != "Alice" {
		t.Errorf("expected Alice first, got %s", all[0].Name)
	}
}

func TestRegistry_MarkSynced(t *testing.T) {
	r := NewRegistry()
	r.Register(&Source{ID: "src-1", Name: "Test", Enabled: true})

	r.MarkSynced("src-1", "ok", 5)

	s := r.Get("src-1")
	if s.LastStatus != "ok" {
		t.Errorf("expected status ok, got %s", s.LastStatus)
	}
	if s.ItemCount != 5 {
		t.Errorf("expected 5 items, got %d", s.ItemCount)
	}
	if s.LastSyncAt.IsZero() {
		t.Error("expected LastSyncAt to be set")
	}
}

func TestListDueForSync(t *testing.T) {
	r := NewRegistry()
	r.Register(&Source{ID: "1", Name: "Due", Enabled: true, AutoSync: true, SyncEvery: "hourly"})
	r.Register(&Source{ID: "2", Name: "Disabled", Enabled: false, AutoSync: true})
	r.Register(&Source{ID: "3", Name: "Recent", Enabled: true, AutoSync: true, LastSyncAt: time.Now(), SyncEvery: "hourly"})

	due := r.ListDueForSync()
	if len(due) != 1 {
		t.Errorf("expected 1 due (never synced), got %d", len(due))
	}
}

func TestRegistry_Stats(t *testing.T) {
	r := NewRegistry()
	r.Register(&Source{ID: "1", Name: "A", Kind: KindFolder, Enabled: true, LastStatus: "ok"})
	r.Register(&Source{ID: "2", Name: "B", Kind: KindGitHub, Enabled: false, LastStatus: "error"})
	r.Register(&Source{ID: "3", Name: "C", Kind: KindFolder, Enabled: true, LastStatus: "ok"})

	stats := r.Stats()
	if stats["total"].(int) != 3 {
		t.Errorf("expected 3 total, got %v", stats["total"])
	}
	if stats["enabled"].(int) != 2 {
		t.Errorf("expected 2 enabled, got %v", stats["enabled"])
	}
	if stats["errors"].(int) != 1 {
		t.Errorf("expected 1 error, got %v", stats["errors"])
	}
}

func TestSource_Summary(t *testing.T) {
	s := &Source{
		Kind:       KindGitHub,
		Name:       "MyRepo",
		Target:     "user/repo",
		ItemCount:  42,
		LastStatus: "ok",
		Enabled:    true,
	}
	summary := s.Summary()
	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestFormatList(t *testing.T) {
	r := NewRegistry()
	output := FormatList(r.List())
	if output == "" {
		t.Error("expected output even for empty list")
	}

	r.Register(&Source{ID: "1", Name: "Test", Kind: KindFolder})
	output = FormatList(r.List())
	if output == "" {
		t.Error("expected non-empty output")
	}
}

func TestParseInterval(t *testing.T) {
	if parseInterval("hourly") != time.Hour {
		t.Error("hourly should be 1 hour")
	}
	if parseInterval("daily") != 24*time.Hour {
		t.Error("daily should be 24 hours")
	}
	if parseInterval("6h") != 6*time.Hour {
		t.Error("6h should be 6 hours")
	}
	if parseInterval("") != 0 {
		t.Error("empty should be 0")
	}
}
