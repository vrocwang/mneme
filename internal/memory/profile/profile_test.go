package profile

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestEnsureSchemaAndUpsert(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	// Idempotent.
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema (2nd): %v", err)
	}

	s := NewStore(db)
	facet := &ProfileFacet{
		FacetType:  FacetPreference,
		Key:        "preferred_language",
		Value:      "Rust",
		Confidence: 0.6,
	}
	if err := s.UpsertFacet(facet); err != nil {
		t.Fatalf("UpsertFacet: %v", err)
	}

	got, err := s.GetByKey(string(FacetPreference), "preferred_language")
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if got == nil || got.Value != "Rust" {
		t.Fatalf("expected facet value Rust, got %+v", got)
	}
	if got.EvidenceCount != 1 {
		t.Errorf("expected evidence count 1, got %d", got.EvidenceCount)
	}

	// Upserting again increments evidence count.
	if err := s.UpsertFacet(&ProfileFacet{FacetType: FacetPreference, Key: "preferred_language", Value: "Rust", Confidence: 0.7}); err != nil {
		t.Fatalf("UpsertFacet (2nd): %v", err)
	}
	got2, _ := s.GetByKey(string(FacetPreference), "preferred_language")
	if got2.EvidenceCount != 2 {
		t.Errorf("expected evidence count 2, got %d", got2.EvidenceCount)
	}
}
