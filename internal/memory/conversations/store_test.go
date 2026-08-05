package conversations

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestStore_CreateAndListThreads(t *testing.T) {
	db := openDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	store.CreateThread("t1", "Test Thread", "")
	store.CreateThread("t2", "Another", "")

	threads, err := store.ListThreads(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) < 2 {
		t.Errorf("expected at least 2 threads, got %d", len(threads))
	}
}

func TestStore_AddAndGetMessages(t *testing.T) {
	db := openDB(t)
	store, _ := NewStore(db)
	store.CreateThread("t1", "Messages", "")

	store.AddMessage("t1", "user", "hello")
	store.AddMessage("t1", "assistant", "hi there")

	msgs, err := store.GetMessages("t1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected user, got %s", msgs[0].Role)
	}
}

func TestStore_SearchMessages(t *testing.T) {
	db := openDB(t)
	store, _ := NewStore(db)
	store.CreateThread("t1", "Search", "")
	store.AddMessage("t1", "user", "looking for blockchain data")

	msgs, err := store.SearchMessages("blockchain", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Error("expected search results")
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
