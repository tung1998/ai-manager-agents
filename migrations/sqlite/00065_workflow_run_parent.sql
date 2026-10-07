-- A workflow may call another (a role filled by a sub-workflow, ADR-102):
-- the run it was called from, and how deep it is (0 = called from a chat).

-- +goose Up
ALTER TABLE workflow_runs ADD COLUMN parent_run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_runs ADD COLUMN depth INTEGER NOT NULL DEFAULT 0;
CREATE INDEX workflow_runs_parent ON workflow_runs (parent_run_id);

-- +goose Down
DROP INDEX workflow_runs_parent;
ALTER TABLE workflow_runs DROP COLUMN depth;
ALTER TABLE workflow_runs DROP COLUMN parent_run_id;
