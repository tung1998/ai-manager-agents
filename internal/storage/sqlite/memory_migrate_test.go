package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"

	"bitbucket.org/senprints/agent-office/migrations"
)

// 00082: the notes kept before topics (ADR-134) are all core.
func TestMemoriesBecomeCore(t *testing.T) {
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
	if _, err := p.UpTo(ctx, 81); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO repos (id, name, path, created_at, updated_at) VALUES ('rep_1', 'r', '/tmp/r', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO agent_memories (id, project_id, agent_id, text, source, created_by, created_at, updated_at)
		 VALUES ('mem_1', 'rep_1', 'agt_1', 'Repo dùng pnpm', 'person', '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := s.Memories().List(ctx, "rep_1", "agt_1")
	if err != nil || len(list) != 1 || list[0].Text != "Repo dùng pnpm" || list[0].Topic != "" || list[0].Summary != "" {
		t.Fatalf("notes = %+v, %v", list, err)
	}
}
