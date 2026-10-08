-- +goose Up
-- A piece's review can fail with a system error (an agent gone, a bad
-- profile, the network), not just the AI connection's limit: counted here so
-- it is failed instead of looping forever (ADR-120).
ALTER TABLE burn_items ADD COLUMN review_err_attempts INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE burn_items DROP COLUMN review_err_attempts;
