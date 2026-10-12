-- +goose Up
-- a Burn's run plan (JSON): how long a run lasts once on, and when it turns
-- on by itself (days of the week, a time); '{}' = until the weekly reset, by hand.
ALTER TABLE burn_sessions ADD COLUMN run_plan TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE burn_sessions DROP COLUMN run_plan;
