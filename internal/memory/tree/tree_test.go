package tree

import "testing"

func TestTree_AddAndGet(t *testing.T) {
	tr := NewTree(10)
	node, err := tr.Add("root", "n1", "content about AI")
	if err != nil {
		t.Fatal(err)
	}
	if node.ID != "n1" {
		t.Errorf("expected n1, got %s", node.ID)
	}
	if node.ParentID != "root" {
		t.Errorf("expected root parent, got %s", node.ParentID)
	}

	found := tr.Get("n1")
	if found == nil {
		t.Error("expected to find n1")
	}
}

func TestTree_Seal(t *testing.T) {
	tr := NewTree(10)
	tr.Add("root", "n1", "item 1")
	tr.Add("root", "n2", "item 2")
	tr.Seal("root", "summary of two items")

	node := tr.Get("root")
	if node.SealedAt == nil {
		t.Error("expected root to be sealed")
	}
	if node.Summary != "summary of two items" {
		t.Errorf("unexpected summary: %s", node.Summary)
	}
}

func TestTree_Search(t *testing.T) {
	tr := NewTree(10)
	tr.Add("root", "n1", "blockchain scalability research")
	tr.Add("root", "n2", "weather forecast for tomorrow")
	tr.Add("n1", "n3", "layer 2 solutions for blockchain")

	results := tr.Search("blockchain", 10)
	if len(results) < 2 {
		t.Errorf("expected at least 2 results, got %d", len(results))
	}
}

func TestTree_ListByParent(t *testing.T) {
	tr := NewTree(10)
	tr.Add("root", "c1", "child 1")
	tr.Add("root", "c2", "child 2")

	children := tr.ListByParent("root")
	if len(children) != 2 {
		t.Errorf("expected 2 children, got %d", len(children))
	}
}
