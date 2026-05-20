package server

import (
	"testing"
	"time"
)

func TestHubRegisterUnregister(t *testing.T) {
	h := NewHub()
	go h.Run()

	c := &clientConn{username: "alice", send: make(chan envelope, 4)}
	h.register <- c

	time.Sleep(20 * time.Millisecond)

	online := h.OnlineUsers()
	if len(online) != 1 || online[0] != "alice" {
		t.Fatalf("expected [alice] online, got %v", online)
	}

	h.unregister <- c
	time.Sleep(20 * time.Millisecond)

	if len(h.OnlineUsers()) != 0 {
		t.Fatal("expected empty online list after unregister")
	}
}

func TestHubBSTAllUsersSorted(t *testing.T) {
	h := NewHub()
	go h.Run()

	for _, u := range []string{"oleg", "alice", "bob", "zara"} {
		h.RegisterUser(u)
	}

	got := h.AllUsersSorted()
	want := []string{"alice", "bob", "oleg", "zara"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: %v", got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("pos %d: got %q, want %q", i, got[i], v)
		}
	}
}

func TestHubDeliverOnline(t *testing.T) {
	h := NewHub()
	go h.Run()

	bob := &clientConn{username: "bob", send: make(chan envelope, 4)}
	h.register <- bob
	h.RegisterUser("bob")
	time.Sleep(20 * time.Millisecond)

	payload := []byte(`{"to_user":"bob","content":"hi"}`)
	h.route <- routeMsg{from: "alice", t: 0x04, payload: payload}
	time.Sleep(20 * time.Millisecond)

	select {
	case env := <-bob.send:
		if string(env.payload) != string(payload) {
			t.Errorf("unexpected payload: %s", env.payload)
		}
	default:
		t.Fatal("bob did not receive message")
	}
}

func TestHubDeliverOfflineDropped(t *testing.T) {
	h := NewHub()
	go h.Run()

	// charlie is not connected — message should be silently dropped
	payload := []byte(`{"to_user":"charlie","content":"hi"}`)
	h.route <- routeMsg{from: "alice", t: 0x04, payload: payload}
	time.Sleep(20 * time.Millisecond)
	// no panic == pass
}
