-- +goose Up
-- Data collections (ADR-172): structured data a project keeps in office's
-- database without code: a collection is a typed schema, its records JSON.
-- A pack declares its own (support: customers, playbooks). Every change of a
-- record keeps what it was in record_history. Text fields are searched with
-- accents ignored, as messages are.
CREATE TABLE collections (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    key         TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    pack        TEXT NOT NULL DEFAULT '',
    fields      TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(fields)),
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (project_id, key)
);
CREATE TABLE records (
    id            TEXT PRIMARY KEY,
    collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    project_id    TEXT NOT NULL,
    data          TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(data)),
    text          TEXT NOT NULL DEFAULT '',
    created_by    TEXT NOT NULL DEFAULT '',
    updated_by    TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX records_collection ON records(collection_id, updated_at);
CREATE INDEX records_project ON records(project_id);
CREATE TABLE record_history (
    id            TEXT PRIMARY KEY,
    record_id     TEXT NOT NULL,
    collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    op            TEXT NOT NULL, -- create | update | delete
    data          TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(data)), -- the record after a create/update, before a delete
    by            TEXT NOT NULL DEFAULT '',
    at            TEXT NOT NULL
);
CREATE INDEX record_history_record ON record_history(record_id, at);
CREATE VIRTUAL TABLE records_fts USING fts5(text, content='records', content_rowid='rowid', tokenize='unicode61 remove_diacritics 2');

-- +goose StatementBegin
CREATE TRIGGER records_fts_insert AFTER INSERT ON records BEGIN
    INSERT INTO records_fts(rowid, text) VALUES (new.rowid, new.text);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER records_fts_delete AFTER DELETE ON records BEGIN
    INSERT INTO records_fts(records_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER records_fts_update AFTER UPDATE OF text ON records BEGIN
    INSERT INTO records_fts(records_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
    INSERT INTO records_fts(rowid, text) VALUES (new.rowid, new.text);
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER records_fts_update;
DROP TRIGGER records_fts_delete;
DROP TRIGGER records_fts_insert;
DROP TABLE records_fts;
DROP TABLE record_history;
DROP TABLE records;
DROP TABLE collections;
