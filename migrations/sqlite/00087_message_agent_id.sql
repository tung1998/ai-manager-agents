-- +goose Up
-- answers() counted an agent's messages by its current Name, which drifts
-- when an agent is renamed or two agents share a Name; store the AgentID
-- that sent each message instead.
ALTER TABLE messages ADD COLUMN agent_id TEXT NOT NULL DEFAULT '';

-- backfill from the run that produced each old assistant message (runs.agent_id);
-- messages without a run_id (e.g. workflow results) are left blank rather than
-- guessed from Author, which would reintroduce the rename/duplicate-name bug.
UPDATE messages SET agent_id = COALESCE(
	(SELECT r.agent_id FROM runs r WHERE r.id = messages.run_id), '')
WHERE role = 'assistant' AND agent_id = '' AND run_id IS NOT NULL;

-- +goose Down
ALTER TABLE messages DROP COLUMN agent_id;
