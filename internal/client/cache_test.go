package client

import (
	"fmt"
	"testing"

	"github.com/wincentaina/go_messenger_ng/internal/protocol"
)

func TestCacheAddAndGet(t *testing.T) {
	c := NewMessageCache(3)

	for i := 1; i <= 3; i++ {
		c.Add(protocol.RecvMsg{FromUser: "alice", ToUser: "bob", Content: fmt.Sprintf("msg%d", i)})
	}

	msgs := c.Get("alice", "bob")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}

func TestCacheEviction(t *testing.T) {
	c := NewMessageCache(2)
	c.Add(protocol.RecvMsg{FromUser: "alice", ToUser: "bob", Content: "old"})
	c.Add(protocol.RecvMsg{FromUser: "alice", ToUser: "bob", Content: "mid"})
	c.Add(protocol.RecvMsg{FromUser: "alice", ToUser: "bob", Content: "new"})

	msgs := c.Get("alice", "bob")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 after eviction, got %d", len(msgs))
	}
	if msgs[0].Content != "mid" || msgs[1].Content != "new" {
		t.Errorf("wrong messages after eviction: %v", msgs)
	}
}

func TestCacheKeySymmetry(t *testing.T) {
	// alice→bob and bob→alice should share the same cache bucket
	c := NewMessageCache(10)
	c.Add(protocol.RecvMsg{FromUser: "alice", ToUser: "bob", Content: "hi"})
	c.Add(protocol.RecvMsg{FromUser: "bob", ToUser: "alice", Content: "hey"})

	if len(c.Get("alice", "bob")) != 2 {
		t.Error("expected both messages in the same conversation bucket")
	}
	if len(c.Get("bob", "alice")) != 2 {
		t.Error("key lookup should be order-independent")
	}
}

func TestGroupCache(t *testing.T) {
	c := NewMessageCache(10)
	c.Add(protocol.RecvMsg{FromUser: "alice", ToGroup: "dev", Content: "hello group"})

	msgs := c.GetGroup("dev")
	if len(msgs) != 1 || msgs[0].Content != "hello group" {
		t.Errorf("group cache: unexpected result %v", msgs)
	}
}
