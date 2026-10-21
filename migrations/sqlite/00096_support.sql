-- +goose Up
-- The support pack (ADR-171): where customers' messages come from, tickets
-- and each ticket's history. Idle while no project has the pack on.
CREATE TABLE support_sources (
    id            TEXT PRIMARY KEY,
    project_id    TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL, -- api | form | crisp | bot
    name          TEXT NOT NULL DEFAULT '',
    automation_id TEXT NOT NULL DEFAULT '', -- the automation (chat or workflow) that answers
    mode          TEXT NOT NULL DEFAULT 'approve', -- auto | approve
    token_hash    TEXT NOT NULL DEFAULT '',
    config        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(config)),
    rate_per_min  INTEGER NOT NULL DEFAULT 30,
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX support_sources_project ON support_sources(project_id);
CREATE TABLE support_tickets (
    id                TEXT PRIMARY KEY,
    project_id        TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    source_id         TEXT NOT NULL REFERENCES support_sources(id) ON DELETE CASCADE,
    external_thread   TEXT NOT NULL DEFAULT '',
    customer_id       TEXT NOT NULL DEFAULT '', -- a record of the customers collection
    subject           TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'new', -- new open waiting_customer waiting_approval resolved closed
    case_type         TEXT NOT NULL DEFAULT '',
    assignee          TEXT NOT NULL DEFAULT '',
    priority          TEXT NOT NULL DEFAULT '',
    tags              TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags)),
    first_response_at TEXT,
    resolved_at       TEXT,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);
CREATE INDEX support_tickets_project ON support_tickets(project_id, status, updated_at);
CREATE INDEX support_tickets_thread ON support_tickets(source_id, external_thread);
CREATE TABLE support_events (
    id              TEXT PRIMARY KEY,
    ticket_id       TEXT NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    source_id       TEXT NOT NULL,
    kind            TEXT NOT NULL, -- customer_msg ai_draft sent note status
    body            TEXT NOT NULL DEFAULT '',
    actor           TEXT NOT NULL DEFAULT '',
    external_id     TEXT NOT NULL DEFAULT '',
    job_id          TEXT NOT NULL DEFAULT '',
    conversation_id TEXT NOT NULL DEFAULT '',
    reply_to        TEXT NOT NULL DEFAULT '', -- the customer message it answers
    state           TEXT NOT NULL DEFAULT '', -- a draft: pending approved rejected superseded failed
    meta            TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(meta)),
    created_at      TEXT NOT NULL
);
CREATE INDEX support_events_ticket ON support_events(ticket_id, created_at);
CREATE INDEX support_events_job ON support_events(job_id) WHERE job_id != '';
-- a message the sender sends again is one message
CREATE UNIQUE INDEX support_events_external ON support_events(source_id, external_id) WHERE external_id != '';

-- +goose Down
DROP TABLE support_events;
DROP TABLE support_tickets;
DROP TABLE support_sources;
