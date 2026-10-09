-- +goose Up
-- ADR-135: a Burn waits while review_cap of its pieces are not merged yet
-- (the person's review is the ceiling), and finishes after stop_after
-- pieces done in a run; 0 = no limit.
ALTER TABLE burn_sessions ADD COLUMN review_cap INTEGER NOT NULL DEFAULT 0;
ALTER TABLE burn_sessions ADD COLUMN stop_after INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE burn_sessions DROP COLUMN stop_after;
ALTER TABLE burn_sessions DROP COLUMN review_cap;
