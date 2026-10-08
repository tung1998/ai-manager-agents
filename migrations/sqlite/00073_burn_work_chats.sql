-- +goose Up
-- Burn pieces work in hidden chats of their own (ADR-116): the Burn's chat is
-- for its scans and the person; what the scans looked at is kept here, not in
-- that chat's history.
ALTER TABLE burn_items ADD COLUMN work_conversation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE burn_sessions ADD COLUMN scanned TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE burn_sessions DROP COLUMN scanned;
ALTER TABLE burn_items DROP COLUMN work_conversation_id;
