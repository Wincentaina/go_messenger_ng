-- Allow system messages (join/leave/etc) to have no sender
ALTER TABLE messages ALTER COLUMN from_user_id DROP NOT NULL;
