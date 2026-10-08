-- +goose Up
-- Burn reviews run hidden (ADR-114): each piece's own review chats, of their
-- own kind (not in the chat list, nobody writes there), read from the Burn.
ALTER TABLE burn_items ADD COLUMN review_conversations TEXT NOT NULL DEFAULT '{}'; -- stage → its review chat
UPDATE conversations SET purpose = 'burn_review'
 WHERE id IN (SELECT j.value FROM burn_sessions s, json_each(s.review_conversations) j);
ALTER TABLE burn_sessions DROP COLUMN review_conversations;

-- +goose Down
ALTER TABLE burn_sessions ADD COLUMN review_conversations TEXT NOT NULL DEFAULT '{}';
UPDATE conversations SET purpose = 'burn' WHERE purpose = 'burn_review';
ALTER TABLE burn_items DROP COLUMN review_conversations;
