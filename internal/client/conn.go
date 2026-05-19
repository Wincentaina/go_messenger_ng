// Package client manages the TLS connection to the server and message I/O.
package client

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"github.com/wincentaina/go_messenger_ng/internal/protocol"
)

// Incoming holds a decoded server message ready for the UI layer.
type Incoming struct {
	Type    protocol.MsgType
	Payload []byte
}

// Conn wraps the TLS connection and exposes channels for the UI.
//
// Two goroutines run after Connect:
//   - reader: server → c.incoming
//   - writer: c.outgoing → server
type Conn struct {
	conn     net.Conn
	incoming chan Incoming
	outgoing chan outMsg
	done     chan struct{} // closed when the connection is dead
	once     sync.Once    // ensures Close is idempotent
}

type outMsg struct {
	t       protocol.MsgType
	payload any
}

// Connect dials the server over TLS and starts the read/write goroutines.
func Connect(addr string, tlsCfg *tls.Config) (*Conn, error) {
	raw, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	c := &Conn{
		conn:     raw,
		incoming: make(chan Incoming, 64),
		outgoing: make(chan outMsg, 64),
		done:     make(chan struct{}),
	}

	go c.readLoop()
	go c.writeLoop()
	return c, nil
}

// Auth sends credentials and waits for the server's AuthResp.
func (c *Conn) Auth(username, password string, register bool) (protocol.AuthResp, error) {
	req := protocol.AuthReq{Username: username, Password: password, Register: register}
	if err := protocol.Encode(c.conn, protocol.TypeAuthReq, req); err != nil {
		return protocol.AuthResp{}, fmt.Errorf("send auth: %w", err)
	}

	// Auth response arrives before the goroutines start routing, so read directly.
	t, raw, err := protocol.Decode(c.conn)
	if err != nil {
		return protocol.AuthResp{}, fmt.Errorf("read auth resp: %w", err)
	}
	if t != protocol.TypeAuthResp {
		return protocol.AuthResp{}, fmt.Errorf("expected auth_resp, got %#x", t)
	}

	var resp protocol.AuthResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return protocol.AuthResp{}, fmt.Errorf("parse auth resp: %w", err)
	}
	return resp, nil
}

// Send queues a message for delivery to the server.
func (c *Conn) Send(t protocol.MsgType, payload any) {
	select {
	case c.outgoing <- outMsg{t: t, payload: payload}:
	case <-c.done:
	}
}

// Incoming returns the channel of messages received from the server.
func (c *Conn) Incoming() <-chan Incoming { return c.incoming }

// Done is closed when the connection is terminated (server closed, network error).
func (c *Conn) Done() <-chan struct{} { return c.done }

// Close shuts down the connection gracefully.
func (c *Conn) Close() {
	c.once.Do(func() {
		c.conn.Close()
		close(c.done)
	})
}

// readLoop runs in a goroutine: reads frames from the wire and pushes to c.incoming.
func (c *Conn) readLoop() {
	defer c.Close()
	for {
		t, raw, err := protocol.Decode(c.conn)
		if err != nil {
			return // connection closed or error
		}
		select {
		case c.incoming <- Incoming{Type: t, Payload: raw}:
		case <-c.done:
			return
		}
	}
}

// writeLoop runs in a goroutine: drains c.outgoing and encodes frames to wire.
func (c *Conn) writeLoop() {
	defer c.Close()
	for {
		select {
		case msg := <-c.outgoing:
			if err := protocol.Encode(c.conn, msg.t, msg.payload); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}
