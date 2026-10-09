-- +goose Up
-- Two-tier memory (ADR-134): a note with a topic is loaded on demand (recall),
-- its summary a line of the index every conversation gets; no topic = core,
-- always loaded as before (the notes there are all core).
ALTER TABLE agent_memories ADD COLUMN topic TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_memories ADD COLUMN summary TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE agent_memories DROP COLUMN summary;
ALTER TABLE agent_memories DROP COLUMN topic;
