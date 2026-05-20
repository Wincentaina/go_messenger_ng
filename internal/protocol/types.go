// Package protocol defines the wire format used between client and server.
//
// Frame layout (7-byte header + JSON payload):
//
//	┌──────────┬───────────┬──────────────┬──────────────────┐
//	│ 2 bytes  │  1 byte   │   4 bytes    │     N bytes      │
//	│  magic   │ msg_type  │    length    │   JSON payload   │
//	│ 0xAB 0xCD│  (MsgType)│  big-endian  │                  │
//	└──────────┴───────────┴──────────────┴──────────────────┘
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	Magic1 byte = 0xAB
	Magic2 byte = 0xCD

	HeaderSize = 7 // 2 magic + 1 type + 4 length
	MaxPayload = 1 << 20 // 1 MiB hard limit per message
)

// MsgType identifies what kind of message is inside the frame.
type MsgType byte

const (
	TypeAuthReq      MsgType = 0x01
	TypeAuthResp     MsgType = 0x02
	TypeSendMsg      MsgType = 0x03
	TypeRecvMsg      MsgType = 0x04
	TypeHistoryReq   MsgType = 0x05
	TypeHistoryResp  MsgType = 0x06
	TypeUserListReq  MsgType = 0x07
	TypeUserListResp MsgType = 0x08
	TypeCreateGroup  MsgType = 0x09
	TypeGroupMsg     MsgType = 0x0A
	TypeError        MsgType = 0x0B
	TypeServerShutdown MsgType = 0x0C
)

// --- Payload structs ---------------------------------------------------------

// AuthReq is sent by the client to log in or register.
type AuthReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Register bool   `json:"register,omitempty"` // true = create new account
}

// AuthResp is the server's reply to AuthReq.
type AuthResp struct {
	OK      bool   `json:"ok"`
	UserID  int    `json:"user_id,omitempty"`
	Message string `json:"message,omitempty"` // error text on failure
}

// SendMsg is sent by the client to deliver a message.
// Exactly one of ToUser / ToGroup must be set.
type SendMsg struct {
	ToUser    string `json:"to_user,omitempty"`
	ToGroup   string `json:"to_group,omitempty"`
	Content   string `json:"content"`
	ReplyToID int64  `json:"reply_to_id,omitempty"`
}

// RecvMsg is delivered to the recipient by the server.
type RecvMsg struct {
	ID        int64  `json:"id"`
	FromUser  string `json:"from_user"`
	ToUser    string `json:"to_user,omitempty"`
	ToGroup   string `json:"to_group,omitempty"`
	Content   string `json:"content"`
	ReplyToID int64  `json:"reply_to_id,omitempty"`
	SentAt    string `json:"sent_at"` // RFC3339
}

// HistoryReq asks for the last N messages in a conversation.
type HistoryReq struct {
	WithUser  string `json:"with_user,omitempty"`
	WithGroup string `json:"with_group,omitempty"`
	Limit     int    `json:"limit"` // max messages to return
}

// HistoryResp carries a slice of past messages.
type HistoryResp struct {
	Messages []RecvMsg `json:"messages"`
}

// UserListReq asks the server for a list of known users.
type UserListReq struct{}

// UserListResp carries all registered users, who is online, and the caller's groups.
type UserListResp struct {
	Users  []string `json:"users"`         // all registered
	Online []string `json:"online"`        // currently connected
	Groups []string `json:"groups"`        // groups the requesting user belongs to
}

// CreateGroup creates a new group chat.
type CreateGroup struct {
	Name    string   `json:"name"`
	Members []string `json:"members,omitempty"` // initial members besides creator
}

// GroupMsg is used for both sending and receiving group messages.
type GroupMsg struct {
	ID        int64  `json:"id,omitempty"` // assigned by server after saving
	Group     string `json:"group"`
	Content   string `json:"content"`
	FromUser  string `json:"from_user,omitempty"` // filled by server on delivery
	SentAt    string `json:"sent_at,omitempty"`
	ReplyToID int64  `json:"reply_to_id,omitempty"`
}

// ErrorMsg carries an error description from the server.
type ErrorMsg struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message"`
}

// ServerShutdown is broadcast when the server is going down.
type ServerShutdown struct {
	Reason string `json:"reason"`
}

// --- Encode / Decode ---------------------------------------------------------

// Encode writes a framed message to w.
func Encode(w io.Writer, t MsgType, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("protocol encode: %w", err)
	}
	if len(data) > MaxPayload {
		return fmt.Errorf("protocol encode: payload too large (%d bytes)", len(data))
	}

	hdr := [HeaderSize]byte{
		Magic1,
		Magic2,
		byte(t),
		byte(len(data) >> 24),
		byte(len(data) >> 16),
		byte(len(data) >> 8),
		byte(len(data)),
	}
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("protocol encode: write header: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("protocol encode: write payload: %w", err)
	}
	return nil
}

// Decode reads one framed message from r.
// Returns the message type and raw JSON payload.
func Decode(r io.Reader) (MsgType, []byte, error) {
	var hdr [HeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, fmt.Errorf("protocol decode: read header: %w", err)
	}

	if hdr[0] != Magic1 || hdr[1] != Magic2 {
		return 0, nil, fmt.Errorf("protocol decode: bad magic %#x %#x", hdr[0], hdr[1])
	}

	msgType := MsgType(hdr[2])
	length := binary.BigEndian.Uint32(hdr[3:7])

	if length > MaxPayload {
		return 0, nil, fmt.Errorf("protocol decode: payload too large (%d bytes)", length)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, fmt.Errorf("protocol decode: read payload: %w", err)
	}

	return msgType, payload, nil
}
