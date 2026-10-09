-- +goose Up
-- ADR-131: a Burn's checks (commands office runs on a piece reported done,
-- one a line; "" = guessed from the project), the lessons its scans keep of
-- what was turned down or failed, and the files each piece changed.
ALTER TABLE burn_sessions ADD COLUMN verify TEXT NOT NULL DEFAULT '';
ALTER TABLE burn_sessions ADD COLUMN lessons TEXT NOT NULL DEFAULT '';
ALTER TABLE burn_items ADD COLUMN files TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE burn_items DROP COLUMN files;
ALTER TABLE burn_sessions DROP COLUMN lessons;
ALTER TABLE burn_sessions DROP COLUMN verify;
