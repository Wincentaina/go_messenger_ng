package server

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/wincentaina/go_messenger_ng/internal/protocol"
	"github.com/wincentaina/go_messenger_ng/internal/server/config"
)

// DB is the interface the server needs from the database layer.
// Defined here so the server package stays decoupled from a specific driver.
type DB interface {
	CreateUser(username, password string) (int, error)
	CheckPassword(username, password string) (int, bool, error)
	SaveMessage(msg protocol.RecvMsg) (int64, error)
	GetHistory(userA, userB string, limit int) ([]protocol.RecvMsg, error)
	ListUsers() ([]string, error)
	CreateGroup(name, createdBy string, members []string) error
	GetGroupMembers(name string) ([]string, error)
	SaveGroupMessage(msg protocol.GroupMsg) (int64, error)
	GetGroupHistory(group string, limit int) ([]protocol.RecvMsg, error)
	GetUserGroups(username string) ([]string, error)
	AddGroupMember(groupName, username string) error
	LeaveGroup(groupName, username string) error
	SoftDeleteUser(username string) error
	SaveLog(eventType, username, details string) error
}

// Logger is the interface the server uses to record events.
type Logger interface {
	Log(eventType, username, details string)
}

// Server ties together the TLS listener, hub, DB, and logger.
type Server struct {
	cfg    config.Config
	hub    *Hub
	db     DB
	logger Logger
}

func New(cfg config.Config, db DB, logger Logger) *Server {
	return &Server{cfg: cfg, hub: NewHub(), db: db, logger: logger}
}

// Run starts the hub, listens for connections, and blocks until SIGTERM/SIGINT.
// On SIGTERM/SIGINT it sends a shutdown notice to all clients and exits cleanly.
// On SIGHUP it reloads the server config (port/limits) without stopping.
func (s *Server) Run(tlsCfg *tls.Config) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)
	ln, err := tls.Listen("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	defer ln.Close()

	// Preload all registered usernames into the BST index on startup
	if users, err := s.db.ListUsers(); err == nil {
		for _, u := range users {
			s.hub.RegisterUser(u)
		}
		log.Printf("BST: loaded %d users", len(users))
	}

	go s.hub.Run()
	s.logger.Log("SERVER_START", "", fmt.Sprintf("addr=%s", addr))
	log.Printf("server listening on %s (TLS)", addr)

	// Accept loop runs in background; signals handled in main goroutine below.
	go s.acceptLoop(ln)

	return s.handleSignals(ln)
}

func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener was closed — normal during shutdown
			return
		}
		go s.handleConn(conn)
	}
}

// handleSignals blocks waiting for OS signals.
func (s *Server) handleSignals(ln net.Listener) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	for sig := range sigCh {
		switch sig {
		case syscall.SIGHUP:
			// Reload config on SIGHUP without downtime
			log.Println("SIGHUP: reloading config (feature stub)")
			s.logger.Log("CONFIG_RELOAD", "", "SIGHUP received")

		case syscall.SIGTERM, syscall.SIGINT:
			log.Println("shutdown signal received")
			s.shutdown(ln)
			return nil
		}
	}
	return nil
}

// broadcastUserList sends the updated user+online list to all connected clients.
// Pass newClient when a user just connected — they may not be in hub.clients yet
// due to the async registration channel, so we send to them directly and include
// them in the online set manually.
// broadcastUserList sends a personalised UserListResp to every connected client.
// Each client gets their own group list (groups differ per user).
// Pass newClient when a freshly authenticated user may not be in hub.clients yet.
func (s *Server) broadcastUserList(newClient ...*clientConn) {
	users := s.hub.AllUsersSorted()

	s.hub.mu.RLock()
	onlineSet := make(map[string]bool, len(s.hub.clients)+1)
	for name := range s.hub.clients {
		onlineSet[name] = true
	}
	var extra *clientConn
	if len(newClient) > 0 && newClient[0] != nil {
		extra = newClient[0]
		onlineSet[extra.username] = true
	}
	online := make([]string, 0, len(onlineSet))
	for name := range onlineSet {
		online = append(online, name)
	}

	// Send personalised response to each client (different groups per user)
	for _, c := range s.hub.clients {
		groups, _ := s.db.GetUserGroups(c.username)
		raw, _ := json.Marshal(protocol.UserListResp{Users: users, Online: online, Groups: groups})
		select {
		case c.send <- envelope{t: protocol.TypeUserListResp, payload: raw}:
		default:
		}
	}
	s.hub.mu.RUnlock()

	if extra != nil {
		groups, _ := s.db.GetUserGroups(extra.username)
		raw, _ := json.Marshal(protocol.UserListResp{Users: users, Online: online, Groups: groups})
		select {
		case extra.send <- envelope{t: protocol.TypeUserListResp, payload: raw}:
		default:
		}
	}
}

// shutdown broadcasts a notice to all clients and closes the listener.
func (s *Server) shutdown(ln net.Listener) {
	notice, _ := json.Marshal(protocol.ServerShutdown{Reason: "сервер пал, милорд"})

	var wg sync.WaitGroup
	s.hub.mu.RLock()
	for _, c := range s.hub.clients {
		wg.Add(1)
		go func(c *clientConn) {
			defer wg.Done()
			select {
			case c.send <- envelope{t: protocol.TypeServerShutdown, payload: notice}:
			default:
			}
		}(c)
	}
	s.hub.mu.RUnlock()
	wg.Wait()

	ln.Close()
	s.logger.Log("SERVER_STOP", "", "graceful shutdown")
	log.Println("server stopped")
}
