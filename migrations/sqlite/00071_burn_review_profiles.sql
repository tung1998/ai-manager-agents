-- +goose Up
-- Burn review profiles (ADR-113): saved review setups of a project, each
-- stage with its own reviewer; a Burn picks one (none = no review).
CREATE TABLE burn_review_profiles (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    stages      TEXT NOT NULL DEFAULT '{}', -- {"issue"|"plan"|"result": {"agent_id","workflow"}}; absent = not reviewed
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
CREATE INDEX burn_review_profiles_project ON burn_review_profiles (project_id, name);

ALTER TABLE burn_sessions ADD COLUMN review_profile_id TEXT NOT NULL DEFAULT '';
ALTER TABLE burn_sessions ADD COLUMN review_conversations TEXT NOT NULL DEFAULT '{}'; -- stage → its review chat

-- the setup of ADR-112 becomes a profile
INSERT INTO burn_review_profiles (id, project_id, name, stages, created_at, updated_at)
SELECT 'brp_' || substr(id, 5), project_id, 'Mặc định',
       (SELECT json_group_object(value, json_object('agent_id', s.review_agent_id, 'workflow', s.review_workflow))
          FROM json_each('["' || replace(s.review_stages, ',', '","') || '"]')),
       updated_at, updated_at
  FROM burn_sessions s WHERE review_stages <> '';
UPDATE burn_sessions SET review_profile_id = 'brp_' || substr(id, 5) WHERE review_stages <> '';

ALTER TABLE burn_sessions DROP COLUMN review_stages;
ALTER TABLE burn_sessions DROP COLUMN review_agent_id;
ALTER TABLE burn_sessions DROP COLUMN review_workflow;
ALTER TABLE burn_sessions DROP COLUMN review_conversation_id;

-- +goose Down
ALTER TABLE burn_sessions ADD COLUMN review_stages TEXT NOT NULL DEFAULT '';
ALTER TABLE burn_sessions ADD COLUMN review_agent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE burn_sessions ADD COLUMN review_workflow TEXT NOT NULL DEFAULT '';
ALTER TABLE burn_sessions ADD COLUMN review_conversation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE burn_sessions DROP COLUMN review_conversations;
ALTER TABLE burn_sessions DROP COLUMN review_profile_id;
DROP TABLE burn_review_profiles;
