-- No more org models (ADR-099): agents belong to the project, the project
-- has a default agent, and how agents work together is a workflow.
--   agents.org_model_id → agents.project_id (library templates' agents dropped)
--   tier, reports_to    → gone; the first lead (by sort) becomes repos.default_agent_id
--   governance.notes    → appended to the default agent's instructions
--   governance council  → the "hoi-dong-3-ben" workflow installed for the
--                         project, its roles bound to the first three leads
--                         (source filled in at start-up from the built-in)
--   org_revisions       → revisions of the project's agents
-- Tables are rebuilt with foreign keys off, otherwise dropping the old ones
-- would cascade.

-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys = OFF;

ALTER TABLE repos ADD COLUMN default_agent_id TEXT NOT NULL DEFAULT '';

UPDATE repos SET default_agent_id = COALESCE((
    SELECT a.id FROM agents a JOIN org_models o ON o.id = a.org_model_id
    WHERE o.repo_id = repos.id
    ORDER BY CASE a.tier WHEN 'lead' THEN 0 WHEN 'manager' THEN 1 ELSE 2 END, a.sort, a.created_at LIMIT 1
), '');

CREATE TABLE agents_new (
    id                    TEXT PRIMARY KEY,
    project_id            TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    key                   TEXT NOT NULL,
    name                  TEXT NOT NULL,
    role                  TEXT NOT NULL DEFAULT '',
    description           TEXT NOT NULL DEFAULT '',
    provider_id           TEXT REFERENCES providers(id) ON DELETE SET NULL,
    model_tier            TEXT NOT NULL DEFAULT 'balanced' CHECK (model_tier IN ('strong', 'balanced', 'fast')),
    llm_model             TEXT NOT NULL DEFAULT '',   -- overrides the provider's tier model when set
    instructions          TEXT NOT NULL DEFAULT '',
    permissions           TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(permissions)),
    sort                  INTEGER NOT NULL DEFAULT 0,
    avatar                TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(avatar)),
    enabled               INTEGER NOT NULL DEFAULT 1,
    fallback_provider_ids TEXT NOT NULL DEFAULT '[]',
    effort                TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    UNIQUE (project_id, key)
);
INSERT INTO agents_new (id, project_id, key, name, role, description, provider_id, model_tier, llm_model,
                        instructions, permissions, sort, avatar, enabled, fallback_provider_ids, effort, created_at, updated_at)
SELECT a.id, o.repo_id, a.key, a.name, a.role, a.description, a.provider_id, a.model_tier, a.llm_model,
       CASE WHEN r.default_agent_id = a.id AND TRIM(COALESCE(json_extract(o.governance, '$.notes'), '')) <> ''
            THEN TRIM(a.instructions || char(10) || char(10) || 'Cách làm việc của nhóm: ' || json_extract(o.governance, '$.notes'))
            ELSE a.instructions END,
       a.permissions, a.sort, a.avatar, a.enabled, a.fallback_provider_ids, a.effort, a.created_at, a.updated_at
FROM agents a JOIN org_models o ON o.id = a.org_model_id JOIN repos r ON r.id = o.repo_id;

-- a council project keeps its way of deciding as an installed workflow
INSERT INTO workflows (id, project_id, key, name, description, source, source_key, source_hash, bindings, enabled, created_at, updated_at)
SELECT 'wfl_' || lower(hex(randomblob(13))), o.repo_id, 'hoi-dong-3-ben', 'Hội đồng 3 bên', '', '', 'hoi-dong-3-ben', '',
       json_object(
           'lap-ke-hoach', COALESCE((SELECT a.id FROM agents a WHERE a.org_model_id = o.id AND a.tier = 'lead' ORDER BY a.sort, a.created_at LIMIT 1 OFFSET 0), ''),
           'thuc-thi',     COALESCE((SELECT a.id FROM agents a WHERE a.org_model_id = o.id AND a.tier = 'lead' ORDER BY a.sort, a.created_at LIMIT 1 OFFSET 1), ''),
           'giam-sat',     COALESCE((SELECT a.id FROM agents a WHERE a.org_model_id = o.id AND a.tier = 'lead' ORDER BY a.sort, a.created_at LIMIT 1 OFFSET 2), '')),
       1, o.created_at, o.updated_at
FROM org_models o
WHERE o.repo_id IS NOT NULL AND json_extract(o.governance, '$.mode') = 'council'
  AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = o.repo_id AND w.key = 'hoi-dong-3-ben');

DROP TABLE agents;
ALTER TABLE agents_new RENAME TO agents;
CREATE INDEX agents_project ON agents(project_id, sort);

CREATE TABLE revisions (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    action      TEXT NOT NULL,
    actor       TEXT NOT NULL DEFAULT '',
    agent_count INTEGER NOT NULL DEFAULT 0,
    snapshot    TEXT NOT NULL CHECK (json_valid(snapshot)), -- {"agents": [...]}
    created_at  TEXT NOT NULL
);
INSERT INTO revisions (id, project_id, action, actor, agent_count, snapshot, created_at)
SELECT v.id, o.repo_id, v.action, v.actor, v.agent_count, v.snapshot, v.created_at
FROM org_revisions v JOIN org_models o ON o.id = v.org_model_id
WHERE o.repo_id IS NOT NULL;
CREATE INDEX revisions_project ON revisions(project_id, created_at);
DROP TABLE org_revisions;

DROP TABLE org_models;

PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;

CREATE TABLE org_models (
    id                 TEXT PRIMARY KEY,
    repo_id            TEXT REFERENCES repos(id) ON DELETE CASCADE,
    source_template_id TEXT REFERENCES org_models(id) ON DELETE SET NULL,
    key                TEXT NOT NULL,
    name               TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    kind               TEXT NOT NULL CHECK (kind IN ('solo', 'team', 'council', 'custom')),
    governance         TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(governance)),
    builtin            INTEGER NOT NULL DEFAULT 0,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL
);
CREATE UNIQUE INDEX org_models_template_key ON org_models(key) WHERE repo_id IS NULL;
CREATE UNIQUE INDEX org_models_repo ON org_models(repo_id) WHERE repo_id IS NOT NULL;
INSERT INTO org_models (id, repo_id, key, name, kind, governance, created_at, updated_at)
SELECT 'org_' || r.id, r.id, 'custom', r.name, 'custom', '{"mode":"single"}', r.created_at, r.updated_at
FROM repos r WHERE EXISTS (SELECT 1 FROM agents a WHERE a.project_id = r.id);

CREATE TABLE agents_old (
    id            TEXT PRIMARY KEY,
    org_model_id  TEXT NOT NULL REFERENCES org_models(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    name          TEXT NOT NULL,
    tier          TEXT NOT NULL CHECK (tier IN ('lead', 'manager', 'worker')),
    role          TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    reports_to    TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(reports_to)),
    provider_id   TEXT REFERENCES providers(id) ON DELETE SET NULL,
    model_tier    TEXT NOT NULL DEFAULT 'balanced' CHECK (model_tier IN ('strong', 'balanced', 'fast')),
    llm_model     TEXT NOT NULL DEFAULT '',
    instructions  TEXT NOT NULL DEFAULT '',
    permissions   TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(permissions)),
    sort          INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL, avatar TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(avatar)), enabled INTEGER NOT NULL DEFAULT 1, fallback_provider_ids TEXT NOT NULL DEFAULT '[]', effort TEXT NOT NULL DEFAULT '',
    UNIQUE (org_model_id, key)
);
INSERT INTO agents_old (id, org_model_id, key, name, tier, role, description, provider_id, model_tier, llm_model,
                        instructions, permissions, sort, created_at, updated_at, avatar, enabled, fallback_provider_ids, effort)
SELECT a.id, 'org_' || a.project_id, a.key, a.name,
       CASE WHEN r.default_agent_id = a.id THEN 'lead' ELSE 'worker' END,
       a.role, a.description, a.provider_id, a.model_tier, a.llm_model,
       a.instructions, a.permissions, a.sort, a.created_at, a.updated_at, a.avatar, a.enabled, a.fallback_provider_ids, a.effort
FROM agents a JOIN repos r ON r.id = a.project_id;
DROP TABLE agents;
ALTER TABLE agents_old RENAME TO agents;

CREATE TABLE org_revisions (
    id           TEXT PRIMARY KEY,
    org_model_id TEXT NOT NULL REFERENCES org_models(id) ON DELETE CASCADE,
    action       TEXT NOT NULL,
    actor        TEXT NOT NULL DEFAULT '',
    agent_count  INTEGER NOT NULL DEFAULT 0,
    snapshot     TEXT NOT NULL CHECK (json_valid(snapshot)),
    created_at   TEXT NOT NULL
);
CREATE INDEX org_revisions_model ON org_revisions(org_model_id, created_at);
INSERT INTO org_revisions SELECT v.id, 'org_' || v.project_id, v.action, v.actor, v.agent_count, v.snapshot, v.created_at
FROM revisions v WHERE EXISTS (SELECT 1 FROM org_models o WHERE o.id = 'org_' || v.project_id);
DROP TABLE revisions;

ALTER TABLE repos DROP COLUMN default_agent_id;

PRAGMA foreign_keys = ON;
