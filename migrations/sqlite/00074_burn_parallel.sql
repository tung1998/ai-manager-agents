-- +goose Up
-- A Burn works on several pieces at once (ADR-117): the cap on subagents in
-- one piece becomes the cap on pieces at a time (at least one).
ALTER TABLE burn_sessions RENAME COLUMN max_subagents TO max_parallel;
UPDATE burn_sessions SET max_parallel = MIN(MAX(max_parallel, 1), 5);

-- +goose Down
ALTER TABLE burn_sessions RENAME COLUMN max_parallel TO max_subagents;
