-- +goose Up
-- ADR-136: an AI connection stops taking new turns once a usage window passes
-- the percent set here ({"five_hour":95,"seven_day":90}; missing/0 = off),
-- until that window resets — so a run never drains the whole quota.
ALTER TABLE providers ADD COLUMN limit_caps TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE providers DROP COLUMN limit_caps;
