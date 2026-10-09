-- +goose Up
-- One branch or one worktree per Burn run (ADR-123): run_branch names this
-- run's branch (branch mode) or, past "burn/", its worktree (worktree mode).
-- "patch" mode becomes "worktree".
ALTER TABLE burn_sessions ADD COLUMN run_branch TEXT NOT NULL DEFAULT '';
UPDATE burn_sessions SET result_mode = 'worktree' WHERE result_mode = 'patch';

-- +goose Down
UPDATE burn_sessions SET result_mode = 'patch' WHERE result_mode = 'worktree';
ALTER TABLE burn_sessions DROP COLUMN run_branch;
