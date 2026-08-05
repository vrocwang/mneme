package todos

import (
	"testing"
)

func TestStore_AddAndList(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	snap, err := store.Add("thread-1", "Fix the bug", "It crashes on startup")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(snap.Cards))
	}
	if snap.Cards[0].Title != "Fix the bug" {
		t.Errorf("expected 'Fix the bug', got %q", snap.Cards[0].Title)
	}
	if snap.Cards[0].Status != StatusTodo {
		t.Errorf("expected todo status, got %s", snap.Cards[0].Status)
	}
}

func TestStore_UpdateStatus(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	snap, _ := store.Add("thread-1", "Task 1", "")
	cardID := snap.Cards[0].ID

	snap, err := store.UpdateStatus("thread-1", cardID, StatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Cards[0].Status != StatusInProgress {
		t.Errorf("expected in_progress, got %s", snap.Cards[0].Status)
	}
}

func TestStore_Edit(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	snap, _ := store.Add("thread-1", "Original", "")
	cardID := snap.Cards[0].ID

	snap, err := store.Edit("thread-1", cardID, "Updated", "New notes")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Cards[0].Title != "Updated" {
		t.Errorf("expected 'Updated', got %q", snap.Cards[0].Title)
	}
	if snap.Cards[0].Notes != "New notes" {
		t.Errorf("expected 'New notes', got %q", snap.Cards[0].Notes)
	}
}

func TestStore_Remove(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	snap, _ := store.Add("thread-1", "Task 1", "")
	cardID := snap.Cards[0].ID

	snap, err := store.Remove("thread-1", cardID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Cards) != 0 {
		t.Errorf("expected 0 cards after remove, got %d", len(snap.Cards))
	}
}

func TestStore_Clear(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	store.Add("thread-1", "Task 1", "")
	store.Add("thread-1", "Task 2", "")

	if err := store.Clear("thread-1"); err != nil {
		t.Fatal(err)
	}

	snap, _ := store.List("thread-1")
	if len(snap.Cards) != 0 {
		t.Errorf("expected empty board, got %d cards", len(snap.Cards))
	}
}

func TestStore_BoardsAreSeparate(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	store.Add("thread-1", "T1 Task", "")
	store.Add("thread-2", "T2 Task", "")

	snap1, _ := store.List("thread-1")
	snap2, _ := store.List("thread-2")

	if len(snap1.Cards) != 1 {
		t.Errorf("thread-1: expected 1 card, got %d", len(snap1.Cards))
	}
	if len(snap2.Cards) != 1 {
		t.Errorf("thread-2: expected 1 card, got %d", len(snap2.Cards))
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	store1 := NewStore(dir)
	snap, _ := store1.Add("thread-1", "Persistent task", "")
	cardID := snap.Cards[0].ID
	store1.UpdateStatus("thread-1", cardID, StatusDone)

	// Re-open to verify persistence.
	store2 := NewStore(dir)
	snap2, err := store2.List("thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap2.Cards) != 1 {
		t.Fatalf("expected 1 card after reload, got %d", len(snap2.Cards))
	}
	if snap2.Cards[0].Status != StatusDone {
		t.Errorf("expected done after reload, got %s", snap2.Cards[0].Status)
	}
}

func TestRenderMarkdown(t *testing.T) {
	cards := []Card{
		{ID: "1", Title: "Todo task", Status: StatusTodo, Order: 0},
		{ID: "2", Title: "In progress task", Status: StatusInProgress, Order: 1},
		{ID: "3", Title: "Blocked task", Status: StatusBlocked, Order: 2, Notes: "Waiting on API"},
		{ID: "4", Title: "Done task", Status: StatusDone, Order: 3},
	}

	md := renderMarkdown(cards)
	if md == "" {
		t.Error("expected non-empty markdown")
	}
	// Check all sections appear.
	for _, section := range []string{"## Todo", "## In Progress", "## Blocked", "## Done"} {
		if !contains(md, section) {
			t.Errorf("expected section %q in markdown", section)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
