package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const normalUUID = "550e8400-e29b-41d4-a716-446655440000"

// TestCheckID is the table that pins the accepted id shape. The rule is
// deliberately conservative: thread_id becomes a filename component and card_id
// is matched against ids this store generated, so both must be plain.
func TestCheckID(t *testing.T) {
	cases := []struct {
		name string
		kind string
		id   string
		ok   bool
	}{
		{"normal uuid", "thread_id", normalUUID, true},
		{"letters digits dash underscore dot", "thread_id", "thread_1.2-3", true},
		{"empty", "thread_id", "", false},
		{"parent traversal", "thread_id", "../../escape", false},
		{"separator", "thread_id", "a/b", false},
		{"backslash separator", "thread_id", `a\b`, false},
		{"bare dotdot", "thread_id", "..", false},
		{"bare dot", "thread_id", ".", false},
		{"absolute", "thread_id", "/etc/passwd", false},
		{"url-ish colon", "thread_id", "webhook:abc", false},
		{"nul", "thread_id", "a\x00b", false},
		{"normal card id", "card_id", "card-1728561600000000000", true},
		{"card traversal", "card_id", "../card", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkID(tc.kind, tc.id)
			if tc.ok && err != nil {
				t.Fatalf("checkID(%q, %q) = %v, want nil", tc.kind, tc.id, err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatalf("checkID(%q, %q) = nil, want error", tc.kind, tc.id)
				}
				if !strings.Contains(err.Error(), tc.kind) {
					t.Errorf("error %q should name the offending field %q", err, tc.kind)
				}
			}
		})
	}
}

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MNEME_HOME", home)
	s := NewStore()
	return s, filepath.Join(home, "agent_task_boards")
}

// TestStoreRejectsUnsafeThreadID proves the escaping id is refused by every
// entry point and that no board file lands outside the boards directory.
func TestStoreRejectsUnsafeThreadID(t *testing.T) {
	s, dir := newTestStore(t)

	unsafe := "../../escape"
	if _, err := s.List(unsafe); err == nil {
		t.Error("List accepted a traversal thread_id")
	}
	if _, err := s.Add(unsafe, "title", ""); err == nil {
		t.Error("Add accepted a traversal thread_id")
	}
	if _, err := s.Edit(unsafe, "card-1", "t", ""); err == nil {
		t.Error("Edit accepted a traversal thread_id")
	}
	if _, err := s.UpdateStatus(unsafe, "card-1", StatusDone); err == nil {
		t.Error("UpdateStatus accepted a traversal thread_id")
	}
	if _, err := s.Remove(unsafe, "card-1"); err == nil {
		t.Error("Remove accepted a traversal thread_id")
	}
	if err := s.Clear(unsafe); err == nil {
		t.Error("Clear accepted a traversal thread_id")
	}

	// The would-be escape target is <home>/../escape.json: filepath.Join cleans
	// <home>/agent_task_boards/../../escape.json to exactly that. Nothing may
	// exist there.
	if _, err := os.Stat(filepath.Join(dir, "..", "..", "escape.json")); !os.IsNotExist(err) {
		t.Fatalf("a board file escaped the boards directory: stat err = %v", err)
	}
}

// TestStoreRejectsUnsafeCardID covers the card-id half: card ids are matched,
// not turned into paths, but an unsafe one is still rejected rather than
// silently matching nothing.
func TestStoreRejectsUnsafeCardID(t *testing.T) {
	s, _ := newTestStore(t)

	for _, bad := range []string{"../../escape", "a/b", "..", ""} {
		if _, err := s.Edit(normalUUID, bad, "t", ""); err == nil {
			t.Errorf("Edit accepted unsafe card_id %q", bad)
		}
		if _, err := s.UpdateStatus(normalUUID, bad, StatusDone); err == nil {
			t.Errorf("UpdateStatus accepted unsafe card_id %q", bad)
		}
		if _, err := s.Remove(normalUUID, bad); err == nil {
			t.Errorf("Remove accepted unsafe card_id %q", bad)
		}
	}
}

// TestStoreNormalIDsRoundTrip is the control: a normal UUID thread and the
// generated card id still work through the whole CRUD surface.
func TestStoreNormalIDsRoundTrip(t *testing.T) {
	s, dir := newTestStore(t)

	snap, err := s.Add(normalUUID, "first", "notes")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(snap.Cards) != 1 {
		t.Fatalf("Add returned %d cards, want 1", len(snap.Cards))
	}
	cardID := snap.Cards[0].ID

	if snap, err = s.Edit(normalUUID, cardID, "renamed", ""); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if snap.Cards[0].Title != "renamed" {
		t.Errorf("title = %q, want renamed", snap.Cards[0].Title)
	}

	if snap, err = s.UpdateStatus(normalUUID, cardID, StatusDone); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if snap.Cards[0].Status != StatusDone {
		t.Errorf("status = %q, want done", snap.Cards[0].Status)
	}

	if snap, err = s.List(normalUUID); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(snap.Cards) != 1 {
		t.Fatalf("List returned %d cards, want 1", len(snap.Cards))
	}

	if _, err = s.Remove(normalUUID, cardID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if snap, err = s.List(normalUUID); err != nil {
		t.Fatalf("List after remove: %v", err)
	}
	if len(snap.Cards) != 0 {
		t.Fatalf("List after remove returned %d cards, want 0", len(snap.Cards))
	}

	if _, err := os.Stat(filepath.Join(dir, normalUUID+".json")); err != nil {
		t.Fatalf("board file for the normal thread is missing: %v", err)
	}
}
