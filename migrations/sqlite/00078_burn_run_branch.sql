-- +goose Up
-- One worktree on one branch per Burn run (ADR-123): run_branch names the
-- run's branch; its worktree is named after it. result_mode is no longer read.
ALTER TABLE burn_sessions ADD COLUMN run_branch TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE burn_sessions DROP COLUMN run_branch;
