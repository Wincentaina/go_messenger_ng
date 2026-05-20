// Package db wraps all PostgreSQL operations used by the server.
package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq" // postgres driver
	"golang.org/x/crypto/bcrypt"

	"github.com/wincentaina/go_messenger_ng/internal/protocol"
)

// Postgres implements server.DB backed by PostgreSQL.
type Postgres struct {
	conn *sql.DB
}

// Open connects to PostgreSQL and pings it.
func Open(dsn string, maxOpen, maxIdle int) (*Postgres, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("db open: %w", err)
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return &Postgres{conn: db}, nil
}

func (p *Postgres) Close() { p.conn.Close() }

// CreateUser hashes the password and inserts a new user row.
// Returns the new user's ID, or an error if the username is taken.
func (p *Postgres) CreateUser(username, password string) (int, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("hash password: %w", err)
	}
	var id int
	err = p.conn.QueryRow(
		`INSERT INTO users(username, password_hash) VALUES($1,$2) RETURNING id`,
		username, string(hash),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

// CheckPassword verifies credentials. Returns (userID, true, nil) on success.
func (p *Postgres) CheckPassword(username, password string) (int, bool, error) {
	var id int
	var hash string
	err := p.conn.QueryRow(
		`SELECT id, password_hash FROM users WHERE username=$1`, username,
	).Scan(&id, &hash)
	log.Printf("db.CheckPassword: username=%q scan_err=%v", username, err)
	if err == sql.ErrNoRows {
		return 0, false, nil // user genuinely does not exist
	}
	if err != nil {
		return 0, false, fmt.Errorf("lookup user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return 0, false, fmt.Errorf("неверный пароль")
	}
	return id, true, nil
}

// ListUsers returns all registered usernames sorted alphabetically.
func (p *Postgres) ListUsers() ([]string, error) {
	rows, err := p.conn.Query(`SELECT username FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// SaveMessage persists a DM and returns the assigned message ID.
func (p *Postgres) SaveMessage(msg protocol.RecvMsg) (int64, error) {
	var fromID, toID int
	if err := p.conn.QueryRow(`SELECT id FROM users WHERE username=$1`, msg.FromUser).Scan(&fromID); err != nil {
		return 0, fmt.Errorf("sender not found: %w", err)
	}
	if err := p.conn.QueryRow(`SELECT id FROM users WHERE username=$1`, msg.ToUser).Scan(&toID); err != nil {
		return 0, fmt.Errorf("recipient not found: %w", err)
	}

	var replyTo *int64
	if msg.ReplyToID > 0 {
		replyTo = &msg.ReplyToID
	}

	var id int64
	err := p.conn.QueryRow(
		`INSERT INTO messages(from_user_id, to_user_id, content, reply_to_id)
		 VALUES($1,$2,$3,$4) RETURNING id`,
		fromID, toID, msg.Content, replyTo,
	).Scan(&id)
	return id, err
}

// GetHistory returns up to limit DMs between userA and userB, oldest first.
func (p *Postgres) GetHistory(userA, userB string, limit int) ([]protocol.RecvMsg, error) {
	rows, err := p.conn.Query(`
		SELECT m.id, u_from.username, u_to.username, m.content,
		       COALESCE(m.reply_to_id, 0), m.sent_at
		FROM messages m
		JOIN users u_from ON u_from.id = m.from_user_id
		JOIN users u_to   ON u_to.id   = m.to_user_id
		WHERE m.group_id IS NULL
		  AND (
		       (u_from.username=$1 AND u_to.username=$2) OR
		       (u_from.username=$2 AND u_to.username=$1)
		  )
		ORDER BY m.sent_at DESC
		LIMIT $3`,
		userA, userB, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []protocol.RecvMsg
	for rows.Next() {
		var m protocol.RecvMsg
		var ts time.Time
		if err := rows.Scan(&m.ID, &m.FromUser, &m.ToUser, &m.Content, &m.ReplyToID, &ts); err != nil {
			return nil, err
		}
		m.SentAt = ts.UTC().Format(time.RFC3339)
		msgs = append(msgs, m)
	}
	// Reverse so oldest is first
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, rows.Err()
}

// CreateGroup inserts a new group and adds initial members.
func (p *Postgres) CreateGroup(name, createdBy string, members []string) error {
	var creatorID int
	if err := p.conn.QueryRow(`SELECT id FROM users WHERE username=$1`, createdBy).Scan(&creatorID); err != nil {
		return fmt.Errorf("creator not found: %w", err)
	}

	var groupID int
	err := p.conn.QueryRow(
		`INSERT INTO groups(name, created_by) VALUES($1,$2) RETURNING id`, name, creatorID,
	).Scan(&groupID)
	if err != nil {
		return fmt.Errorf("create group: %w", err)
	}

	for _, member := range members {
		var uid int
		if err := p.conn.QueryRow(`SELECT id FROM users WHERE username=$1`, member).Scan(&uid); err != nil {
			continue // skip unknown members
		}
		_, _ = p.conn.Exec(
			`INSERT INTO group_members(group_id, user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`,
			groupID, uid,
		)
	}
	return nil
}

// GetGroupMembers returns usernames of everyone in a group.
func (p *Postgres) GetGroupMembers(name string) ([]string, error) {
	rows, err := p.conn.Query(`
		SELECT u.username
		FROM group_members gm
		JOIN groups g ON g.id = gm.group_id
		JOIN users  u ON u.id = gm.user_id
		WHERE g.name = $1`, name,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// GetUserGroups returns names of all groups the user belongs to.
func (p *Postgres) GetUserGroups(username string) ([]string, error) {
	rows, err := p.conn.Query(`
		SELECT g.name
		FROM groups g
		JOIN group_members gm ON gm.group_id = g.id
		JOIN users u ON u.id = gm.user_id
		WHERE u.username = $1
		ORDER BY g.name`, username,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		groups = append(groups, name)
	}
	return groups, rows.Err()
}

// SaveGroupMessage persists a message sent to a group.
func (p *Postgres) SaveGroupMessage(msg protocol.GroupMsg) error {
	var fromID, groupID int
	if err := p.conn.QueryRow(`SELECT id FROM users WHERE username=$1`, msg.FromUser).Scan(&fromID); err != nil {
		return fmt.Errorf("sender not found: %w", err)
	}
	if err := p.conn.QueryRow(`SELECT id FROM groups WHERE name=$1`, msg.Group).Scan(&groupID); err != nil {
		return fmt.Errorf("group not found: %w", err)
	}
	_, err := p.conn.Exec(
		`INSERT INTO messages(from_user_id, group_id, content) VALUES($1,$2,$3)`,
		fromID, groupID, msg.Content,
	)
	return err
}

// GetGroupHistory returns up to limit messages in a group, oldest first.
func (p *Postgres) GetGroupHistory(groupName string, limit int) ([]protocol.RecvMsg, error) {
	rows, err := p.conn.Query(`
		SELECT m.id, u.username, g.name, m.content, m.sent_at
		FROM messages m
		JOIN users  u ON u.id = m.from_user_id
		JOIN groups g ON g.id = m.group_id
		WHERE g.name = $1
		ORDER BY m.sent_at DESC
		LIMIT $2`, groupName, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []protocol.RecvMsg
	for rows.Next() {
		var m protocol.RecvMsg
		var ts time.Time
		if err := rows.Scan(&m.ID, &m.FromUser, &m.ToGroup, &m.Content, &ts); err != nil {
			return nil, err
		}
		m.SentAt = ts.UTC().Format(time.RFC3339)
		msgs = append(msgs, m)
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, rows.Err()
}
