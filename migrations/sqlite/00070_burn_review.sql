-- +goose Up
-- Burn review (ADR-112): checkpoints a reviewer (an agent, or a workflow it
-- runs) must agree to; none = as before.
ALTER TABLE burn_sessions ADD COLUMN review_stages TEXT NOT NULL DEFAULT '';          -- comma list: issue,plan,result
ALTER TABLE burn_sessions ADD COLUMN review_agent_id TEXT NOT NULL DEFAULT '';        -- '' = the Burn's agent
ALTER TABLE burn_sessions ADD COLUMN review_workflow TEXT NOT NULL DEFAULT '';        -- '' = the agent answers itself
ALTER TABLE burn_sessions ADD COLUMN review_conversation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE burn_items ADD COLUMN reviewed TEXT NOT NULL DEFAULT '';                   -- comma list of stages passed
ALTER TABLE burn_items ADD COLUMN review_note TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE burn_items DROP COLUMN review_note;
ALTER TABLE burn_items DROP COLUMN reviewed;
ALTER TABLE burn_sessions DROP COLUMN review_conversation_id;
ALTER TABLE burn_sessions DROP COLUMN review_workflow;
ALTER TABLE burn_sessions DROP COLUMN review_agent_id;
ALTER TABLE burn_sessions DROP COLUMN review_stages;
