-- What a run gave back by name (a workflow's declared outputs, ADR-103).

-- +goose Up
ALTER TABLE workflow_runs ADD COLUMN outputs TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(outputs));

-- +goose Down
ALTER TABLE workflow_runs DROP COLUMN outputs;
