-- +goose Up
-- a process's own environment (PORT…), over the office's when it runs
ALTER TABLE processes ADD COLUMN env TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE processes DROP COLUMN env;
