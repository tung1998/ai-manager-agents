-- +goose Up
-- Where a Burn's summary goes when it stops (ADR-120): a bot's chat
-- (Discord channel, Telegram chat); empty = only its own chat.
ALTER TABLE burn_sessions ADD COLUMN notify_channel_id TEXT NOT NULL DEFAULT '';
ALTER TABLE burn_sessions ADD COLUMN notify_chat_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE burn_sessions DROP COLUMN notify_chat_id;
ALTER TABLE burn_sessions DROP COLUMN notify_channel_id;
