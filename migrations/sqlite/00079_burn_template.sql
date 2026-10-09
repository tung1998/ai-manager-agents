-- +goose Up
-- A Burn's template (ADR-128): what its workers look for and how a piece is
-- checked; custom: the person's own prompt for it.
ALTER TABLE burn_sessions ADD COLUMN hunt_template TEXT NOT NULL DEFAULT 'general';
ALTER TABLE burn_sessions ADD COLUMN hunt_prompt TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE burn_sessions DROP COLUMN hunt_prompt;
ALTER TABLE burn_sessions DROP COLUMN hunt_template;
