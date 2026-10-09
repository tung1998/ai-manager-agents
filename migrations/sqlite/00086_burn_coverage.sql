-- +goose Up
-- ADR-141: a Burn's coverage plan, the checklist its scans write of every
-- area its template covers ("- [ ]" to look at, "- [x]" looked at), kept
-- whole between scans so the next one goes on where the last stopped.
ALTER TABLE burn_sessions ADD COLUMN coverage TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE burn_sessions DROP COLUMN coverage;
