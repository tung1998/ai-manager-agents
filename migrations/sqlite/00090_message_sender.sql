-- +goose Up
-- who wrote a message from outside office (a Discord/Telegram bot's chat):
-- JSON {kind, id, username, name, private, link}; '' = written in office.
ALTER TABLE messages ADD COLUMN sender TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE messages DROP COLUMN sender;
