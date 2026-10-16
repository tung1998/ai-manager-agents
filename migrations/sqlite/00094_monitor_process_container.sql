-- Monitors watch only office processes and compose containers, one per
-- process or service, turned on from its row in Vận hành: the URL, TCP and
-- heartbeat monitors are gone, with their checks and events. The table is
-- rebuilt without the heartbeat columns (token, last_ping_at) and without the
-- type CHECK (Go checks it), with foreign keys off as SQLite needs.

-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys = OFF;

DELETE FROM monitor_checks WHERE monitor_id IN (SELECT id FROM monitors WHERE type NOT IN ('process', 'container'));
DELETE FROM monitor_events WHERE monitor_id IN (SELECT id FROM monitors WHERE type NOT IN ('process', 'container'));
DELETE FROM monitors WHERE type NOT IN ('process', 'container');

CREATE TABLE monitors_new (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL,                -- process | container
    target          TEXT NOT NULL DEFAULT '',     -- process id | compose service
    config          TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(config)), -- file (container)
    interval_s      INTEGER NOT NULL DEFAULT 60,
    enabled         INTEGER NOT NULL DEFAULT 1,
    ai_enabled      INTEGER NOT NULL DEFAULT 0,
    ai_budget_usd   REAL NOT NULL DEFAULT 0.5,    -- per day, for this monitor's analyses
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'up', 'down')),
    fails           INTEGER NOT NULL DEFAULT 0,   -- consecutive failed checks
    last_checked_at TEXT,
    last_change_at  TEXT,
    last_latency_ms INTEGER NOT NULL DEFAULT 0,
    last_message    TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE (project_id, name)
);
INSERT INTO monitors_new (id, project_id, name, type, target, config, interval_s, enabled, ai_enabled, ai_budget_usd, status, fails,
        last_checked_at, last_change_at, last_latency_ms, last_message, created_at, updated_at)
    SELECT id, project_id, name, type, target, config, interval_s, enabled, ai_enabled, ai_budget_usd, status, fails,
        last_checked_at, last_change_at, last_latency_ms, last_message, created_at, updated_at FROM monitors;
DROP TABLE monitors;
ALTER TABLE monitors_new RENAME TO monitors;

PRAGMA foreign_keys = ON;

-- +goose Down
-- the removed monitors and the heartbeat columns are not brought back
SELECT 1;
