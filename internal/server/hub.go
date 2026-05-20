// Package server contains the connection hub and per-client handler.
package server

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/wincentaina/go_messenger_ng/internal/protocol"
	"github.com/wincentaina/go_messenger_ng/internal/util"
)

// envelope pairs a message type with its raw payload for routing.
type envelope struct {
	t       protocol.MsgType
	payload []byte
}

// clientConn represents an authenticated connected client.
type clientConn struct {
	username string
	send     chan envelope // outbound messages buffered per-client
}

// Hub is the central router: it owns the connected-clients map and routes
// messages between them. Only the Hub goroutine touches the map — no mutex needed.
//
// userIndex is a BST that keeps all *registered* usernames sorted.
// It gives O(log n) lookup and O(n) sorted traversal — useful for UserListResp
// which must return an alphabetically sorted list on every login event.
type Hub struct {
	register   chan *clientConn
	unregister chan *clientConn
	route      chan routeMsg

	mu        sync.RWMutex
	clients   map[string]*clientConn // username → conn (online only)
	userIndex *util.BST              // all registered usernames, sorted
}

// routeMsg is a message arriving from one client destined for another.
type routeMsg struct {
	from    string
	t       protocol.MsgType
	payload []byte
}

func NewHub() *Hub {
	return &Hub{
		register:   make(chan *clientConn, 8),
		unregister: make(chan *clientConn, 8),
		route:      make(chan routeMsg, 256),
		clients:    make(map[string]*clientConn),
		userIndex:  &util.BST{},
	}
}

// Run processes hub events; call in a dedicated goroutine.
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c.username] = c
			h.mu.Unlock()
			log.Printf("hub: %s connected (%d online)", c.username, len(h.clients))

		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c.username]; ok {
				delete(h.clients, c.username)
				close(c.send)
			}
			h.mu.Unlock()
			log.Printf("hub: %s disconnected (%d online)", c.username, len(h.clients))

		case msg := <-h.route:
			h.deliver(msg)
		}
	}
}

// RegisterUser adds a username to the persistent BST index.
// Call this after a user successfully registers or logs in for the first time.
func (h *Hub) RegisterUser(username string) {
	h.userIndex.Insert(username)
}

// RemoveUser removes a username from the BST index (used on account deletion).
func (h *Hub) RemoveUser(username string) {
	h.userIndex.Delete(username)
}

// AllUsersSorted returns all known usernames in alphabetical order using BST inorder traversal.
func (h *Hub) AllUsersSorted() []string {
	return h.userIndex.InOrder()
}

// deliver routes a message to the intended recipient.
func (h *Hub) deliver(msg routeMsg) {
	var target struct {
		ToUser  string `json:"to_user"`
		ToGroup string `json:"to_group"`
	}
	if err := json.Unmarshal(msg.payload, &target); err != nil {
		log.Printf("hub: bad route payload from %s: %v", msg.from, err)
		return
	}

	if target.ToUser != "" {
		h.mu.RLock()
		recipient, ok := h.clients[target.ToUser]
		h.mu.RUnlock()
		if ok {
			select {
			case recipient.send <- envelope{t: protocol.TypeRecvMsg, payload: msg.payload}:
			default:
				log.Printf("hub: send buffer full for %s, dropping message", target.ToUser)
			}
		}
	}
}

// Broadcast sends a message to every connected client except the sender.
func (h *Hub) Broadcast(from string, t protocol.MsgType, payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for name, c := range h.clients {
		if name == from {
			continue
		}
		select {
		case c.send <- envelope{t: t, payload: payload}:
		default:
		}
	}
}

// OnlineUsers returns a snapshot of currently connected usernames.
func (h *Hub) OnlineUsers() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	names := make([]string, 0, len(h.clients))
	for name := range h.clients {
		names = append(names, name)
	}
	return names
}
