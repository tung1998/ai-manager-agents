-- +goose Up
-- The support pack, going out (ADR-174): replies delivered through an
-- outbox (bot, Crisp, email, hook), hooks fired on a ticket's events, the
-- secret a source sends with (Crisp key, SMTP password; sealed), SLA.
ALTER TABLE support_sources ADD COLUMN secret_enc TEXT NOT NULL DEFAULT '';
ALTER TABLE support_tickets ADD COLUMN sla_due_at TEXT;
ALTER TABLE support_tickets ADD COLUMN sla_breached INTEGER NOT NULL DEFAULT 0;
CREATE TABLE support_outbox (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL, -- reply | hook
    ref_id     TEXT NOT NULL, -- reply: the sent event; hook: the hook
    ticket_id  TEXT NOT NULL DEFAULT '',
    event      TEXT NOT NULL DEFAULT '', -- hook: ticket.replied, …
    payload    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload)),
    status     TEXT NOT NULL DEFAULT 'pending', -- pending sent failed
    attempts   INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    next_at    TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX support_outbox_due ON support_outbox(status, next_at);
CREATE INDEX support_outbox_project ON support_outbox(project_id, created_at);
CREATE TABLE support_hooks (
    id            TEXT PRIMARY KEY,
    project_id    TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    name          TEXT NOT NULL DEFAULT '',
    kind          TEXT NOT NULL DEFAULT 'webhook', -- webhook | automation
    url           TEXT NOT NULL DEFAULT '',
    automation_id TEXT NOT NULL DEFAULT '',
    events        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(events)),
    secret_enc    TEXT NOT NULL DEFAULT '',
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX support_hooks_project ON support_hooks(project_id);

-- +goose Down
DROP TABLE support_hooks;
DROP TABLE support_outbox;
ALTER TABLE support_tickets DROP COLUMN sla_breached;
ALTER TABLE support_tickets DROP COLUMN sla_due_at;
ALTER TABLE support_sources DROP COLUMN secret_enc;
