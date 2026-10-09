-- +goose Up
-- A Burn's code map (ADR-130): what its scans learned of the codebase, kept
-- for the next scan and for the workers that do the pieces.
ALTER TABLE burn_sessions ADD COLUMN code_map TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE burn_sessions DROP COLUMN code_map;
