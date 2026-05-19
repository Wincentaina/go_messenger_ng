-- Users: credentials and registration time
CREATE TABLE IF NOT EXISTS users (
    id            SERIAL PRIMARY KEY,
    username      VARCHAR(50)  UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Named group chats
CREATE TABLE IF NOT EXISTS groups (
    id         SERIAL PRIMARY KEY,
    name       VARCHAR(100) NOT NULL,
    created_by INT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Many-to-many: users ↔ groups
CREATE TABLE IF NOT EXISTS group_members (
    group_id  INT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id   INT NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, user_id)
);

-- Both DMs (to_user_id set) and group messages (group_id set).
-- Exactly one target must be non-null — enforced by CHECK.
CREATE TABLE IF NOT EXISTS messages (
    id           SERIAL PRIMARY KEY,
    from_user_id INT  NOT NULL REFERENCES users(id),
    to_user_id   INT  REFERENCES users(id),
    group_id     INT  REFERENCES groups(id),
    content      TEXT NOT NULL,
    reply_to_id  INT  REFERENCES messages(id) ON DELETE SET NULL,
    sent_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT msg_has_single_target CHECK (
        (to_user_id IS NOT NULL AND group_id IS NULL) OR
        (to_user_id IS NULL     AND group_id IS NOT NULL)
    )
);

-- Persistent audit log: server lifecycle + user events
CREATE TABLE IF NOT EXISTS server_logs (
    id         SERIAL PRIMARY KEY,
    event_type VARCHAR(30) NOT NULL,
    user_id    INT REFERENCES users(id) ON DELETE SET NULL,
    details    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Speed up history queries for DMs
CREATE INDEX IF NOT EXISTS idx_messages_dm
    ON messages(from_user_id, to_user_id, sent_at)
    WHERE group_id IS NULL;

-- Speed up history queries for group chats
CREATE INDEX IF NOT EXISTS idx_messages_group
    ON messages(group_id, sent_at)
    WHERE group_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_server_logs_created
    ON server_logs(created_at DESC);
