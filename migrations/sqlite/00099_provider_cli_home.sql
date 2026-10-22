-- +goose Up
-- ADR-178: a Claude Code connection signed in to an account of its own keeps
-- its config folder here (CLAUDE_CONFIG_DIR); '' = the machine's ~/.claude.
ALTER TABLE providers ADD COLUMN cli_home TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE providers DROP COLUMN cli_home;
