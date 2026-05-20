-- Add soft-delete flag to users table.
-- Deleted users are hidden from listings and cannot log in,
-- but their messages remain with sender shown as 'deleted_user'.
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_deleted BOOLEAN NOT NULL DEFAULT FALSE;
