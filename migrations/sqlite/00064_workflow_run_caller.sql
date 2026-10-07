-- A workflow runs in a chat of its own (purpose "workflow_run", not listed);
-- the chat it was called from shows only its input and its output.
--   conversation_id        → the run's own chat
--   caller_conversation_id → the chat that called it ('' = a run from before,
--                            which ran in conversation_id itself)

-- +goose Up
ALTER TABLE workflow_runs ADD COLUMN caller_conversation_id TEXT NOT NULL DEFAULT '';
CREATE INDEX workflow_runs_caller ON workflow_runs (caller_conversation_id, started_at);

-- +goose Down
DROP INDEX workflow_runs_caller;
ALTER TABLE workflow_runs DROP COLUMN caller_conversation_id;
