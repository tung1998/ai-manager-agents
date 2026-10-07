-- A connection kind is checked in Go (storage.ProviderKind.Valid), not by a
-- CHECK that needs a table rebuild for each new kind (gemini_cli, ADR-103).
-- SQLite cannot change a CHECK constraint, so the table is rebuilt with
-- foreign keys off (agents, runs… reference it).

-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys = OFF;

CREATE TABLE providers_new (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE,
    kind            TEXT NOT NULL,              -- anthropic | openai | openai_compatible | claude_cli | codex_cli | gemini_cli
    base_url        TEXT NOT NULL DEFAULT '',
    api_key_enc     TEXT NOT NULL DEFAULT '',   -- AES-GCM, never returned by the API
    api_key_env     TEXT NOT NULL DEFAULT '',   -- alternative: read the key from this env var
    api_key_hint    TEXT NOT NULL DEFAULT '',   -- last 4 chars for display
    tier_models     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(tier_models)), -- {"strong":..,"balanced":..,"fast":..}
    models          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(models)),      -- last fetched model ids
    is_default      INTEGER NOT NULL DEFAULT 0,
    enabled         INTEGER NOT NULL DEFAULT 1,
    status          TEXT NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown', 'ok', 'error')),
    status_detail   TEXT NOT NULL DEFAULT '',
    checked_at      TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    preset          TEXT NOT NULL DEFAULT ''
);
INSERT INTO providers_new (id, name, kind, base_url, api_key_enc, api_key_env, api_key_hint, tier_models, models, is_default, enabled, status, status_detail, checked_at, created_at, updated_at, preset)
    SELECT id, name, kind, base_url, api_key_enc, api_key_env, api_key_hint, tier_models, models, is_default, enabled, status, status_detail, checked_at, created_at, updated_at, preset FROM providers;
DROP TABLE providers;
ALTER TABLE providers_new RENAME TO providers;

PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;

CREATE TABLE providers_old (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE,
    kind            TEXT NOT NULL CHECK (kind IN ('anthropic', 'openai', 'openai_compatible', 'claude_cli', 'codex_cli')),
    base_url        TEXT NOT NULL DEFAULT '',
    api_key_enc     TEXT NOT NULL DEFAULT '',
    api_key_env     TEXT NOT NULL DEFAULT '',
    api_key_hint    TEXT NOT NULL DEFAULT '',
    tier_models     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(tier_models)),
    models          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(models)),
    is_default      INTEGER NOT NULL DEFAULT 0,
    enabled         INTEGER NOT NULL DEFAULT 1,
    status          TEXT NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown', 'ok', 'error')),
    status_detail   TEXT NOT NULL DEFAULT '',
    checked_at      TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    preset          TEXT NOT NULL DEFAULT ''
);
INSERT INTO providers_old SELECT id, name, kind, base_url, api_key_enc, api_key_env, api_key_hint, tier_models, models, is_default, enabled, status, status_detail, checked_at, created_at, updated_at, preset
    FROM providers WHERE kind != 'gemini_cli';
DROP TABLE providers;
ALTER TABLE providers_old RENAME TO providers;

PRAGMA foreign_keys = ON;
