# Development Plan — go_messenger_ng

**Target: 75+ points | Deadline: ~1.5 days from 2026-05-19 evening**

---

## Phase 0 — Project Skeleton (30 min) 🏗️
**Goal:** Compilable repo with infra ready.

Tasks:
- [ ] `go mod init github.com/wincentaina/go_messenger_ng`
- [ ] Directory structure (cmd/, internal/, migrations/, certs/, config/)
- [ ] `docker-compose.yml` — PostgreSQL 16
- [ ] `migrations/001_init.sql` — full schema
- [ ] `config/server.yaml`, `config/client.yaml` — address, DB DSN, cert paths
- [ ] Generate self-signed TLS certs (script or `go generate`)
- [ ] `Dockerfile.server`

**Checkpoint:** `docker-compose up` starts Postgres, `go build ./...` compiles.

**Tests:** `docker-compose up -d && psql` connects successfully.

---

## Phase 1 — Protocol + Server Core (90 min) 🖥️
**Goal:** Server accepts TLS connections, authenticates users, routes DMs.

Tasks:
- [ ] `internal/protocol/` — define all message structs + encode/decode (binary header + JSON payload)
- [ ] `internal/crypto/` — TLS config loader
- [ ] `internal/server/hub.go` — Hub struct: map of connected clients + mutex + broadcast channel
- [ ] `internal/server/client_handler.go` — goroutine per connection: read loop → dispatch to hub
- [ ] `internal/db/users.go` — CreateUser, GetByUsername (bcrypt passwords)
- [ ] Auth flow: `auth_req` → validate → `auth_resp` (ok/error)
- [ ] Message routing: sender → hub → recipient goroutine (if online) or queue
- [ ] `cmd/server/main.go` — TLS listener, signal handling stub

**Checkpoint:** Server starts, logs "listening on :8443".

**Tests:**
```bash
# Two nc/openssl s_client sessions can connect and auth
openssl s_client -connect localhost:8443
```

---

## Phase 2 — Client Core (90 min) 💻
**Goal:** Client connects, authenticates, sends and receives messages via goroutines.

Tasks:
- [ ] `internal/client/conn.go` — TLS dial + read/write goroutines
- [ ] Goroutine A (reader): receives messages from server → passes to UI channel
- [ ] Goroutine B (writer): takes messages from UI input channel → sends to server
- [ ] Auth flow on connect: prompt username/password → send `auth_req` → handle `auth_resp`
- [ ] Basic stdout/stdin I/O (no TUI yet — Phase 5)
- [ ] `cmd/client/main.go`

**Checkpoint:** Two terminal instances — User A sends "hello" → User B receives it in real-time.

**Tests:**
```bash
# Terminal 1: ./client -user alice -pass secret
# Terminal 2: ./client -user bob -pass secret
# Alice: /msg bob Hello!  →  Bob sees: [alice] Hello!
```

---

## Phase 3 — PostgreSQL Layer (90 min) 🗄️
**Goal:** Messages persist in DB, history survives server restart.

Tasks:
- [ ] `internal/db/messages.go` — SaveMessage, GetHistory(userA, userB, limit)
- [ ] `internal/db/groups.go` — CreateGroup, GetGroupMembers, SaveGroupMessage, GetGroupHistory
- [ ] On `recv_msg`: save to DB before routing to recipient
- [ ] `history_req` / `history_resp` message types: client requests last N messages on chat open
- [ ] On client connect: auto-fetch last 50 messages for each conversation

**Checkpoint:** Restart server → history loads correctly from DB.

**Tests:** Unit tests in `internal/db/messages_test.go` (requires running Postgres).

---

## Phase 4 — Logging (45 min) 📋
**Goal:** Persistent logs for server events, user logins, messages.

Tasks:
- [ ] `internal/server/logger.go` — writes to `server_logs` table AND `logs/server.log` file
- [ ] Log events: SERVER_START, SERVER_STOP, USER_LOGIN, USER_LOGOUT, MSG_SENT, MSG_DELIVERED
- [ ] Log format: `2026-05-19T22:00:00Z | EVENT | user=alice | details`

**Checkpoint:** `tail -f logs/server.log` shows real-time events during test.

**Tests:** Check log file and DB entries after login + message exchange.

---

## Phase 5 — TUI with tview (2.5 hours) 🖥️✨
**Goal:** Proper terminal UI — user list, chat panel, input.

Layout:
```
┌──────────────┬─────────────────────────────────┐
│  Users/Chats │  Chat: alice ↔ bob              │
│              │                                  │
│  > bob       │  [10:23] alice: Hello!           │
│    group1    │  [10:23] bob: Hey there          │
│              │                                  │
│              ├─────────────────────────────────┤
│              │  > Type message here...          │
└──────────────┴─────────────────────────────────┘
```

Tasks:
- [ ] `internal/ui/app.go` — tview Application setup
- [ ] `internal/ui/chat_view.go` — scrollable message history
- [ ] `internal/ui/user_list.go` — list of users/groups, select to switch chat
- [ ] `internal/ui/input.go` — text input, Enter to send, commands (/msg, /quit, /group)
- [ ] Wire UI channels to client reader/writer goroutines
- [ ] Commands: `/msg <user> <text>`, `/history`, `/users`, `/quit`

**Checkpoint:** Full UI works, messages appear in real-time in correct chat pane.

**Tests:** Manual — open two clients, verify UI updates without flickering.

---

## Phase 6 — Signal Handlers (45 min) ⚡
**Goal:** Graceful shutdown + SIGHUP config reload (Bonus 9).

Tasks:
- [ ] Server: `SIGTERM`/`SIGINT` → log SERVER_STOP → send `server_shutdown` to all clients → close connections → exit
- [ ] Server: `SIGHUP` → reload `config/server.yaml` without restart (log it)
- [ ] Client: `SIGTERM`/`SIGINT` → graceful disconnect → exit
- [ ] Client: on receiving `server_shutdown` → show "Сервер пал, милорд" → exit cleanly

**Checkpoint:** `kill -SIGTERM $(pidof server)` → clients see shutdown message.

**Checkpoint:** `kill -SIGHUP $(pidof server)` → server logs "config reloaded", keeps running.

**Tests:** Script that sends signals and checks behavior.

---

## Phase 7 — Group Chats (90 min) 👥
**Goal:** Users can create groups and chat in them (Bonus 2).

Tasks:
- [ ] DB: `groups`, `group_members` tables (already in schema)
- [ ] Protocol: `create_group`, `join_group`, `group_msg`, `group_history_req`
- [ ] Server: route group messages to all online members
- [ ] DB: save group messages with `group_id`
- [ ] Client: `/newgroup <name>`, `/joingroup <name>`, group appears in user list
- [ ] UI: group chats in left panel alongside DMs

**Checkpoint:** Three users in a group, message from one appears to all others.

**Tests:** 3-client integration test (can be manual).

---

## Phase 8 — Reply to Messages (45 min) 💬
**Goal:** Reply to a specific message (Bonus 3a).

Tasks:
- [ ] `messages.reply_to_id` FK (already in schema)
- [ ] Protocol: `send_msg` includes optional `reply_to_id`
- [ ] UI: show replied message as quote above the reply
- [ ] Client command: `/reply <message_id> <text>` or select with arrow keys

**Checkpoint:** Reply shows quoted original message in chat view.

---

## Phase 9 — Client Cache + BST (60 min) 🗂️
**Goal:** Client-side message cache (Nagloe dop.) + BST for user list (Bonus 8).

Tasks:
- [ ] `internal/client/cache.go` — in-memory map `conversationKey → []Message` (last 100)
- [ ] On chat open: serve from cache first, then request delta from server
- [ ] `internal/util/bst.go` — simple BST for storing online users (insert/search/inorder)
- [ ] Server uses BST to maintain sorted online user list
- [ ] Document BST choice: O(log n) lookup vs O(n) slice scan

**Checkpoint:** Open chat → no server round-trip if cache is warm.

---

## Phase 10 — Tests (60 min) 🧪
**Goal:** Key unit + integration tests.

Tasks:
- [ ] `internal/protocol/protocol_test.go` — encode/decode round-trip
- [ ] `internal/db/messages_test.go` — save + retrieve messages
- [ ] `internal/util/bst_test.go` — BST operations
- [ ] `internal/server/hub_test.go` — message routing logic
- [ ] Integration smoke test script: `scripts/test_integration.sh`

```bash
go test ./...
```

---

## Phase 11 — Polish + Docs (60 min) 📝
**Goal:** Developer diary, code comments, README, git history clean.

Tasks:
- [ ] `DEVLOG.md` — document key decisions, issues encountered, solutions
- [ ] `README.md` — how to run (docker-compose up, make build, etc.)
- [ ] Code comments on all exported functions and non-obvious logic
- [ ] Verify all commits have meaningful messages
- [ ] Tag v1.0.0

---

## Score estimate at completion

| Phase | Unlocks | Est. points |
|-------|---------|-------------|
| 0-2   | Base functionality | 50 |
| 3-4   | DB + history + logs | +8 |
| 6     | Signal handlers | +5 |
| TLS (Phase 0) | Encryption | +7 |
| Go cross-platform | Bonus 7a | +3 |
| Go UTF-8 | Bonus 10 | +2 |
| Goroutines documented | Bonus 1 | +5 |
| 7     | Group chats | +5 |
| 8     | Reply to messages | +3 |
| 9     | Cache + BST | +5 |
| Code quality | Comments, git, devlog | +5 |
| **Total** | | **~98** |

---

## Priority if running short on time

**Must have (50 base):** Phases 0, 1, 2, 3, 4
**High priority (+25 pts):** Phase 6 (signals), TLS, document goroutines
**Medium priority:** Phase 5 (TUI — better UI = more points)
**Nice to have:** Phases 7, 8, 9

---

## Commands reference

```bash
# Start infrastructure
docker-compose up -d

# Build
go build ./cmd/server -o bin/server
go build ./cmd/client -o bin/client

# Run
./bin/server -config config/server.yaml
./bin/client -config config/client.yaml

# Test
go test ./...

# Generate certs
go run scripts/gen_certs.go
```
