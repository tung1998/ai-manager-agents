-- A proposal approved while an earlier one of its chat still waits is
-- "queued": it runs once those are decided (chat.Drain). Its status is
-- checked in Go, not by a CHECK that needs a table rebuild for each new one.
-- SQLite cannot change a CHECK constraint, so both tables are rebuilt with
-- foreign keys off.

-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys = OFF;

CREATE TABLE patches_new (
    id              TEXT PRIMARY KEY,
    conversation_id TEXT REFERENCES conversations(id) ON DELETE CASCADE,
    message_id      TEXT REFERENCES messages(id) ON DELETE CASCADE,
    task_id         TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    step_id         TEXT REFERENCES task_steps(id) ON DELETE CASCADE,
    diff            TEXT NOT NULL,
    files           TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(files)),
    status          TEXT NOT NULL DEFAULT 'pending', -- pending | queued | applied | rejected | failed
    detail          TEXT NOT NULL DEFAULT '',
    decided_by      TEXT NOT NULL DEFAULT '',
    decided_at      TEXT,
    created_at      TEXT NOT NULL,
    origin          TEXT NOT NULL DEFAULT '',
    tree            TEXT NOT NULL DEFAULT '',
    CHECK (conversation_id IS NOT NULL OR task_id IS NOT NULL)
);
INSERT INTO patches_new (id, conversation_id, message_id, task_id, step_id, diff, files, status, detail, decided_by, decided_at, created_at, origin, tree)
    SELECT id, conversation_id, message_id, task_id, step_id, diff, files, status, detail, decided_by, decided_at, created_at, origin, tree FROM patches;
DROP TABLE patches;
ALTER TABLE patches_new RENAME TO patches;
CREATE INDEX patches_conversation ON patches(conversation_id);
CREATE INDEX patches_task ON patches(task_id);

CREATE TABLE actions_new (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    conversation_id TEXT REFERENCES conversations(id) ON DELETE CASCADE,
    message_id      TEXT,
    task_id         TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    run_ref         TEXT NOT NULL DEFAULT '',
    kind            TEXT NOT NULL,
    target          TEXT NOT NULL,
    target_id       TEXT NOT NULL DEFAULT '',
    reason          TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending', -- pending | queued | done | failed | rejected
    detail          TEXT NOT NULL DEFAULT '',
    proposed_by     TEXT NOT NULL DEFAULT '',
    decided_by      TEXT NOT NULL DEFAULT '',
    decided_at      TEXT,
    created_at      TEXT NOT NULL,
    args            TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(args)),
    job_id          TEXT NOT NULL DEFAULT ''
);
INSERT INTO actions_new (id, project_id, conversation_id, message_id, task_id, run_ref, kind, target, target_id, reason, status, detail, proposed_by, decided_by, decided_at, created_at, args, job_id)
    SELECT id, project_id, conversation_id, message_id, task_id, run_ref, kind, target, target_id, reason, status, detail, proposed_by, decided_by, decided_at, created_at, args, job_id FROM actions;
DROP TABLE actions;
ALTER TABLE actions_new RENAME TO actions;
CREATE INDEX actions_conversation ON actions(conversation_id, created_at);
CREATE INDEX actions_task ON actions(task_id, created_at);
CREATE INDEX actions_run ON actions(run_ref);

PRAGMA foreign_keys = ON;

-- +goose Down
-- queued ones go back to waiting; the CHECKs are not put back (Go checks)
UPDATE patches SET status = 'pending' WHERE status = 'queued';
UPDATE actions SET status = 'pending' WHERE status = 'queued';
