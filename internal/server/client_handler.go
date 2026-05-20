package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/wincentaina/go_messenger_ng/internal/protocol"
)

// handleConn is spawned as a goroutine for each accepted TLS connection.
// It authenticates the user, then runs parallel read/write loops.
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	// --- authentication phase (must complete before anything else) ---
	username, err := s.authenticate(conn)
	if err != nil {
		log.Printf("auth failed from %s: %v", conn.RemoteAddr(), err)
		return
	}

	client := &clientConn{
		username: username,
		send:     make(chan envelope, 64),
	}
	s.hub.register <- client
	defer func() {
		s.hub.unregister <- client
		s.broadcastUserList() // notify everyone that someone went offline
	}()

	s.logger.Log("USER_LOGIN", username, fmt.Sprintf("addr=%s", conn.RemoteAddr()))
	// Pass client explicitly: hub may not have processed the registration yet,
	// so we include the new user in the online set and send directly to them.
	s.broadcastUserList(client)

	// writer goroutine: drains client.send → wire
	done := make(chan struct{})
	go func() {
		defer close(done)
		for env := range client.send {
			if err := protocol.Encode(conn, env.t, json.RawMessage(env.payload)); err != nil {
				log.Printf("write to %s: %v", username, err)
				return
			}
		}
	}()

	// reader loop (this goroutine): wire → hub
	s.readLoop(conn, client)
	s.logger.Log("USER_LOGOUT", username, "")
}

// authenticate performs the auth handshake: reads one AuthReq, validates it,
// writes an AuthResp, and returns the username on success.
func (s *Server) authenticate(conn net.Conn) (string, error) {
	conn.SetDeadline(time.Now().Add(15 * time.Second)) //nolint:errcheck
	defer conn.SetDeadline(time.Time{})                //nolint:errcheck

	t, raw, err := protocol.Decode(conn)
	if err != nil {
		return "", fmt.Errorf("read auth: %w", err)
	}
	if t != protocol.TypeAuthReq {
		return "", fmt.Errorf("expected auth_req, got %#x", t)
	}

	var req protocol.AuthReq
	if err := json.Unmarshal(raw, &req); err != nil {
		return "", fmt.Errorf("parse auth_req: %w", err)
	}
	// Trim on server side regardless of client — prevents trailing-space bugs
	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)

	if req.Username == "" || req.Password == "" {
		_ = protocol.Encode(conn, protocol.TypeAuthResp, protocol.AuthResp{
			OK: false, Message: "логин и пароль не могут быть пустыми",
		})
		return "", fmt.Errorf("empty credentials")
	}

	var (
		userID int
		authOK bool
	)
	if req.Register {
		userID, err = s.db.CreateUser(req.Username, req.Password)
		authOK = err == nil
	} else {
		userID, authOK, err = s.db.CheckPassword(req.Username, req.Password)
	}

	if err != nil || !authOK {
		msg := "неверное имя пользователя или пароль"
		if err != nil {
			msg = err.Error()
		}
		_ = protocol.Encode(conn, protocol.TypeAuthResp, protocol.AuthResp{OK: false, Message: msg})
		return "", fmt.Errorf("%s", msg)
	}

	// Keep BST index up to date (idempotent for existing users)
	s.hub.RegisterUser(req.Username)

	_ = protocol.Encode(conn, protocol.TypeAuthResp, protocol.AuthResp{OK: true, UserID: userID})
	return req.Username, nil
}

// readLoop reads frames from the connection and dispatches them.
func (s *Server) readLoop(conn net.Conn, client *clientConn) {
	for {
		t, raw, err := protocol.Decode(conn)
		if err != nil {
			// EOF or network error — normal on disconnect
			return
		}

		switch t {
		case protocol.TypeSendMsg:
			s.handleSendMsg(client, raw)

		case protocol.TypeHistoryReq:
			s.handleHistoryReq(client, raw)

		case protocol.TypeUserListReq:
			s.handleUserListReq(client)

		case protocol.TypeCreateGroup:
			s.handleCreateGroup(client, raw)

		case protocol.TypeGroupMsg:
			s.handleGroupMsg(client, raw)

		default:
			log.Printf("unknown message type %#x from %s", t, client.username)
		}
	}
}

func (s *Server) handleSendMsg(client *clientConn, raw []byte) {
	var msg protocol.SendMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		s.sendError(client, "invalid send_msg payload")
		return
	}

	// Persist to DB
	recv := protocol.RecvMsg{
		FromUser:  client.username,
		ToUser:    msg.ToUser,
		ToGroup:   msg.ToGroup,
		Content:   msg.Content,
		ReplyToID: msg.ReplyToID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
	}
	id, err := s.db.SaveMessage(recv)
	if err != nil {
		log.Printf("save message: %v", err)
	}
	recv.ID = id

	recvRaw, _ := json.Marshal(recv)

	// Echo back to sender so they see their own message in the chat
	select {
	case client.send <- envelope{t: protocol.TypeRecvMsg, payload: recvRaw}:
	default:
	}

	s.hub.route <- routeMsg{from: client.username, t: protocol.TypeRecvMsg, payload: recvRaw}
	s.logger.Log("MSG_SENT", client.username, fmt.Sprintf("to=%s", msg.ToUser))
}

func (s *Server) handleHistoryReq(client *clientConn, raw []byte) {
	var req protocol.HistoryReq
	if err := json.Unmarshal(raw, &req); err != nil {
		s.sendError(client, "invalid history_req payload")
		return
	}
	if req.Limit <= 0 || req.Limit > 200 {
		req.Limit = 50
	}

	msgs, err := s.db.GetHistory(client.username, req.WithUser, req.Limit)
	if err != nil {
		log.Printf("get history: %v", err)
		msgs = nil
	}

	resp := protocol.HistoryResp{Messages: msgs}
	respRaw, _ := json.Marshal(resp)
	select {
	case client.send <- envelope{t: protocol.TypeHistoryResp, payload: respRaw}:
	default:
	}
}

func (s *Server) handleUserListReq(client *clientConn) {
	// BST.InOrder() gives sorted list in O(n) without hitting the DB
	users := s.hub.AllUsersSorted()
	online := s.hub.OnlineUsers()
	resp := protocol.UserListResp{Users: users, Online: online}
	respRaw, _ := json.Marshal(resp)
	select {
	case client.send <- envelope{t: protocol.TypeUserListResp, payload: respRaw}:
	default:
	}
}

func (s *Server) handleCreateGroup(client *clientConn, raw []byte) {
	var req protocol.CreateGroup
	if err := json.Unmarshal(raw, &req); err != nil || req.Name == "" {
		s.sendError(client, "invalid create_group payload")
		return
	}
	members := append(req.Members, client.username)
	if err := s.db.CreateGroup(req.Name, client.username, members); err != nil {
		s.sendError(client, fmt.Sprintf("create group: %v", err))
		return
	}
	log.Printf("group %q created by %s", req.Name, client.username)
}

func (s *Server) handleGroupMsg(client *clientConn, raw []byte) {
	var msg protocol.GroupMsg
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Group == "" {
		s.sendError(client, "invalid group_msg payload")
		return
	}

	members, err := s.db.GetGroupMembers(msg.Group)
	if err != nil {
		s.sendError(client, "group not found")
		return
	}

	msg.FromUser = client.username
	msg.SentAt = time.Now().UTC().Format(time.RFC3339)

	if err := s.db.SaveGroupMessage(msg); err != nil {
		log.Printf("save group message: %v", err)
	}

	outRaw, _ := json.Marshal(msg)
	s.hub.mu.RLock()
	for _, member := range members {
		if member == client.username {
			continue
		}
		if c, ok := s.hub.clients[member]; ok {
			select {
			case c.send <- envelope{t: protocol.TypeGroupMsg, payload: outRaw}:
			default:
			}
		}
	}
	s.hub.mu.RUnlock()
}

func (s *Server) sendError(client *clientConn, msg string) {
	raw, _ := json.Marshal(protocol.ErrorMsg{Message: msg})
	select {
	case client.send <- envelope{t: protocol.TypeError, payload: raw}:
	default:
	}
}
