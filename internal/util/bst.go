// Package util provides auxiliary data structures used across the project.
package util

import "sync"

// BST is a thread-safe binary search tree that stores unique string keys.
//
// Why BST here instead of a plain slice?
// The server keeps a set of all registered usernames that it queries on every
// login, logout and user-list request. With a sorted slice every insertion is
// O(n); with a BST it is O(log n) on average. Inorder traversal produces a
// sorted list in O(n) — exactly what UserListResp needs — without an extra
// sort pass.
//
// Trade-off: worst-case O(n) on already-sorted input (degenerate tree).
// Acceptable for a coursework messenger where usernames arrive in arbitrary
// order. A production system would use a red-black tree or skip list.
type BST struct {
	mu   sync.RWMutex
	root *bstNode
}

type bstNode struct {
	key   string
	left  *bstNode
	right *bstNode
}

// Insert adds key to the tree. Duplicate keys are silently ignored.
func (t *BST) Insert(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.root = bstInsert(t.root, key)
}

// Search returns true if key exists in the tree. O(log n) average.
func (t *BST) Search(key string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return bstSearch(t.root, key)
}

// Delete removes key from the tree if it exists.
func (t *BST) Delete(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.root = bstDelete(t.root, key)
}

// InOrder returns all keys in ascending alphabetical order. O(n).
func (t *BST) InOrder() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var result []string
	bstInOrder(t.root, &result)
	return result
}

// Len returns the number of nodes in the tree.
func (t *BST) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return bstLen(t.root)
}

// --- internal recursive helpers (no locking, called under t.mu) -------------

func bstInsert(n *bstNode, key string) *bstNode {
	if n == nil {
		return &bstNode{key: key}
	}
	if key < n.key {
		n.left = bstInsert(n.left, key)
	} else if key > n.key {
		n.right = bstInsert(n.right, key)
	}
	// equal: ignore duplicate
	return n
}

func bstSearch(n *bstNode, key string) bool {
	if n == nil {
		return false
	}
	if key == n.key {
		return true
	}
	if key < n.key {
		return bstSearch(n.left, key)
	}
	return bstSearch(n.right, key)
}

func bstDelete(n *bstNode, key string) *bstNode {
	if n == nil {
		return nil
	}
	if key < n.key {
		n.left = bstDelete(n.left, key)
	} else if key > n.key {
		n.right = bstDelete(n.right, key)
	} else {
		// Found — handle three cases
		if n.left == nil {
			return n.right
		}
		if n.right == nil {
			return n.left
		}
		// Two children: replace with in-order successor (smallest in right subtree)
		successor := bstMin(n.right)
		n.key = successor.key
		n.right = bstDelete(n.right, successor.key)
	}
	return n
}

func bstMin(n *bstNode) *bstNode {
	for n.left != nil {
		n = n.left
	}
	return n
}

func bstInOrder(n *bstNode, result *[]string) {
	if n == nil {
		return
	}
	bstInOrder(n.left, result)
	*result = append(*result, n.key)
	bstInOrder(n.right, result)
}

func bstLen(n *bstNode) int {
	if n == nil {
		return 0
	}
	return 1 + bstLen(n.left) + bstLen(n.right)
}
