-- +goose Up
-- an account's own ids on the bots' platforms: a message to a bot from one
-- of them is shown as that account's.
ALTER TABLE users ADD COLUMN discord_id TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN telegram_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN telegram_id;
ALTER TABLE users DROP COLUMN discord_id;
