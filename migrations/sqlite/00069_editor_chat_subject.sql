-- +goose Up
-- The skill or workflow an editor's chat writes (not listed; found again by
-- it): "skill:<scope>:<name>", "wf:<id>", "lib:<key>".
ALTER TABLE conversations ADD COLUMN subject TEXT NOT NULL DEFAULT '';
CREATE INDEX conversations_subject ON conversations(project_id, purpose, subject) WHERE subject != '';

-- +goose Down
DROP INDEX conversations_subject;
ALTER TABLE conversations DROP COLUMN subject;
