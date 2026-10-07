-- +goose Up
-- Workflows (spec 2026-10-07-workflows-design): how a project's agents work
-- together. A project's copy of a library workflow: its whole file, where it
-- came from, and which agent fills each role.
CREATE TABLE workflows (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    key         TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL,
    source_key  TEXT NOT NULL DEFAULT '',
    source_hash TEXT NOT NULL DEFAULT '',
    bindings    TEXT NOT NULL DEFAULT '{}',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (project_id, key)
);

-- One run of a workflow in a chat: its roles (agent, session, rounds), gates
-- and log as JSON, written as it goes.
CREATE TABLE workflow_runs (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    conversation_id  TEXT NOT NULL,
    workflow_id      TEXT NOT NULL DEFAULT '',
    workflow_key     TEXT NOT NULL,
    workflow_name    TEXT NOT NULL,
    body_hash        TEXT NOT NULL DEFAULT '',
    coordinator_id   TEXT NOT NULL DEFAULT '',
    coordinator_name TEXT NOT NULL DEFAULT '',
    input            TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL,
    turns            INTEGER NOT NULL DEFAULT 0,
    cost_usd         REAL NOT NULL DEFAULT 0,
    result           TEXT NOT NULL DEFAULT '',
    error            TEXT NOT NULL DEFAULT '',
    roles            TEXT NOT NULL DEFAULT '[]',
    gates            TEXT NOT NULL DEFAULT '[]',
    log              TEXT NOT NULL DEFAULT '[]',
    actor            TEXT NOT NULL DEFAULT '',
    started_at       TEXT NOT NULL,
    finished_at      TEXT
);
CREATE INDEX workflow_runs_conversation ON workflow_runs (conversation_id, started_at);
CREATE INDEX workflow_runs_project ON workflow_runs (project_id, started_at);

-- +goose Down
DROP TABLE workflow_runs;
DROP TABLE workflows;
