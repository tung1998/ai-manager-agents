-- +goose Up
-- Messages written while a chat answers: kept by office (not the browser) and
-- sent together as the next message once the chat is free.
CREATE TABLE chat_queued (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    author TEXT NOT NULL DEFAULT '',
    text TEXT NOT NULL DEFAULT '',
    attachments TEXT NOT NULL DEFAULT '[]',
    context TEXT NOT NULL DEFAULT '',
    options TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);
CREATE INDEX chat_queued_conversation ON chat_queued(conversation_id, created_at);

-- +goose Down
DROP TABLE chat_queued;
