package util

import (
	"testing"
)

func TestInsertAndSearch(t *testing.T) {
	tree := &BST{}
	for _, name := range []string{"oleg", "alice", "ivan", "bob"} {
		tree.Insert(name)
	}

	for _, name := range []string{"oleg", "alice", "ivan", "bob"} {
		if !tree.Search(name) {
			t.Errorf("Search(%q) = false, want true", name)
		}
	}
	if tree.Search("missing") {
		t.Error("Search(missing) = true, want false")
	}
}

func TestInOrderSorted(t *testing.T) {
	tree := &BST{}
	for _, name := range []string{"oleg", "alice", "ivan", "bob"} {
		tree.Insert(name)
	}

	got := tree.InOrder()
	want := []string{"alice", "bob", "ivan", "oleg"}
	if len(got) != len(want) {
		t.Fatalf("InOrder len: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("InOrder[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDuplicateIgnored(t *testing.T) {
	tree := &BST{}
	tree.Insert("alice")
	tree.Insert("alice")
	tree.Insert("alice")
	if tree.Len() != 1 {
		t.Errorf("Len after 3 duplicate inserts: got %d, want 1", tree.Len())
	}
}

func TestDelete(t *testing.T) {
	tree := &BST{}
	for _, name := range []string{"alice", "bob", "ivan"} {
		tree.Insert(name)
	}

	tree.Delete("bob")
	if tree.Search("bob") {
		t.Error("bob still found after Delete")
	}
	// Others must remain
	if !tree.Search("alice") || !tree.Search("ivan") {
		t.Error("other nodes unexpectedly removed")
	}
	if tree.Len() != 2 {
		t.Errorf("Len after delete: got %d, want 2", tree.Len())
	}
}

func TestDeleteRoot(t *testing.T) {
	tree := &BST{}
	tree.Insert("m")
	tree.Insert("a")
	tree.Insert("z")
	tree.Delete("m") // delete root with two children
	got := tree.InOrder()
	if len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Errorf("after root delete InOrder = %v, want [a z]", got)
	}
}
