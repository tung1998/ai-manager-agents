-- +goose Up
-- Asking an expert (ADR-177): one row per question. side "out" is the
-- question a chat here asked (its answer comes back to that chat), "in" one
-- an agent here answers (a question from another office, or from a project
-- of this office: then one row is both, side "local").
CREATE TABLE consults (
    id              TEXT PRIMARY KEY,
    side            TEXT NOT NULL,              -- out | in | local
    peer_id         TEXT NOT NULL DEFAULT '',   -- the other office ('' = this one)
    remote_id       TEXT NOT NULL DEFAULT '',   -- in: the asking office's id of it
    conversation_id TEXT NOT NULL DEFAULT '',   -- out/local: the asking chat
    project_id      TEXT NOT NULL DEFAULT '',   -- out/local: the asking project
    caller_agent_id TEXT NOT NULL DEFAULT '',   -- out/local: the agent that asked
    actor           TEXT NOT NULL DEFAULT '',   -- the person it runs for
    ceiling         TEXT NOT NULL DEFAULT '',   -- the asking turn's ceiling
    root_message_id TEXT NOT NULL DEFAULT '',   -- the person's message the chain started from
    expert_project  TEXT NOT NULL DEFAULT '',   -- the expert's project (id; another office's when out)
    expert_agent_id TEXT NOT NULL DEFAULT '',
    expert_name     TEXT NOT NULL DEFAULT '',   -- as shown with the answer
    expert_project_name TEXT NOT NULL DEFAULT '',
    from_label      TEXT NOT NULL DEFAULT '',   -- in: who asks ("office/project · agent")
    question        TEXT NOT NULL,
    consult_conversation_id TEXT NOT NULL DEFAULT '', -- in/local: the expert's hidden chat
    status          TEXT NOT NULL DEFAULT 'pending', -- pending answered failed
    answer          TEXT NOT NULL DEFAULT '',
    error           TEXT NOT NULL DEFAULT '',
    deadline        TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    done_at         TEXT
);
CREATE INDEX consults_conversation ON consults(conversation_id, status);
CREATE INDEX consults_consult_conv ON consults(consult_conversation_id, status);
CREATE UNIQUE INDEX consults_remote ON consults(peer_id, remote_id) WHERE side = 'in';
CREATE INDEX consults_status ON consults(status, deadline);

-- +goose Down
DROP TABLE consults;
