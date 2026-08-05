package entities

import "testing"

func TestRegistry_UpsertAndSearch(t *testing.T) {
	r, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	r.Upsert(Entity{Name: "Bitcoin", Kind: KindTopic, Description: "A decentralized cryptocurrency"})
	r.Upsert(Entity{Name: "Alice", Kind: KindPerson, Description: "A user"})

	results := r.Search("bitcoin", 10)
	if len(results) == 0 {
		t.Error("expected to find Bitcoin entity")
	}

	e, err := r.Get(KindTopic, "Bitcoin")
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "Bitcoin" {
		t.Errorf("expected Bitcoin, got %s", e.Name)
	}
}

func TestExtractFromText(t *testing.T) {
	text := "Alice discussed Bitcoin and Ethereum with @bob at https://example.com"
	entities := ExtractFromText(text)

	if len(entities) == 0 {
		t.Error("expected extracted entities")
	}

	// Should find @bob
	found := false
	for _, e := range entities {
		if e.Name == "bob" && e.Kind == KindPerson {
			found = true
		}
	}
	if !found {
		t.Error("expected to extract @bob as person")
	}
}
