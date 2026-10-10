package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"

	"bitbucket.org/senprints/agent-office/migrations"
)

// 00087: old assistant messages had no agent_id; the migration backfills it
// from the run that produced them (runs.agent_id), via messages.run_id.
func TestMessageAgentIDBackfillsFromRun(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := goose.NewProvider(goose.DialectSQLite3, s.db, mustSub(migrations.SQLite, "sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 86); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO repos (id, name, path, created_at, updated_at) VALUES ('rep_1', 'r', '/tmp/r', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO agents (id, project_id, key, name, created_at, updated_at) VALUES ('agt_1', 'rep_1', 'a', 'Agent A', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO conversations (id, project_id, agent_id, created_at, updated_at) VALUES ('conv_1', 'rep_1', 'agt_1', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO runs (id, kind, project_id, agent_id, status, created_at) VALUES ('run_1', 'chat', 'rep_1', 'agt_1', 'ok', '2026-01-01T00:00:00Z')`,
		// old assistant message: has a run_id but no agent_id (column doesn't exist yet pre-00087)
		`INSERT INTO messages (id, conversation_id, role, content, run_id, author, created_at) VALUES ('msg_1', 'conv_1', 'assistant', 'hi', 'run_1', 'Agent A', '2026-01-01T00:00:00Z')`,
		// a workflow-result message: no run_id, should stay blank, not guessed from Author
		`INSERT INTO messages (id, conversation_id, role, content, author, created_at) VALUES ('msg_2', 'conv_1', 'assistant', 'kết quả', 'Agent A', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	msgs, err := s.Chat().ListMessages(ctx, "conv_1")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for _, m := range msgs {
		byID[m.ID] = m.AgentID
	}
	if byID["msg_1"] != "agt_1" {
		t.Fatalf("msg_1 agent_id = %q, want agt_1", byID["msg_1"])
	}
	if byID["msg_2"] != "" {
		t.Fatalf("msg_2 agent_id = %q, want blank (no run_id to backfill from)", byID["msg_2"])
	}
}
