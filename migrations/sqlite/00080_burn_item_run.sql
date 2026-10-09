-- +goose Up
-- The run a piece belongs to (its run's branch), so the board shows one run
-- at a time. Pieces from before: the run the Burn has now, if they came
-- after it started; else none (shown under "all").
ALTER TABLE burn_items ADD COLUMN run_branch TEXT NOT NULL DEFAULT '';
UPDATE burn_items SET run_branch = (SELECT s.run_branch FROM burn_sessions s WHERE s.id = burn_items.session_id)
WHERE created_at >= COALESCE((SELECT s.started_at FROM burn_sessions s WHERE s.id = burn_items.session_id), '9999');

-- +goose Down
ALTER TABLE burn_items DROP COLUMN run_branch;
