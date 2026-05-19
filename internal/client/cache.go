package client

import "github.com/wincentaina/go_messenger_ng/internal/protocol"

// MessageCache stores recent messages per conversation in memory.
// Key is "userA:userB" (sorted) for DMs, or "group:name" for groups.
//
// Using a map of slices (acting as a bounded ring-buffer per chat) gives
// O(1) lookup by conversation key — better than scanning a flat slice.
type MessageCache struct {
	data  map[string][]protocol.RecvMsg
	limit int
}

func NewMessageCache(limit int) *MessageCache {
	if limit <= 0 {
		limit = 100
	}
	return &MessageCache{data: make(map[string][]protocol.RecvMsg), limit: limit}
}

// Add appends a message to its conversation bucket, evicting the oldest if full.
func (c *MessageCache) Add(msg protocol.RecvMsg) {
	key := cacheKey(msg)
	msgs := c.data[key]
	if len(msgs) >= c.limit {
		msgs = msgs[1:] // drop oldest
	}
	c.data[key] = append(msgs, msg)
}

// Get returns cached messages for a DM conversation.
func (c *MessageCache) Get(userA, userB string) []protocol.RecvMsg {
	key := dmKey(userA, userB)
	return c.data[key]
}

// GetGroup returns cached messages for a group.
func (c *MessageCache) GetGroup(group string) []protocol.RecvMsg {
	return c.data["group:"+group]
}

func cacheKey(msg protocol.RecvMsg) string {
	if msg.ToGroup != "" {
		return "group:" + msg.ToGroup
	}
	return dmKey(msg.FromUser, msg.ToUser)
}

// dmKey produces a stable key regardless of sender/receiver order.
func dmKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + ":" + b
}
