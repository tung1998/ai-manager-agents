package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"

	"bitbucket.org/senprints/agent-office/migrations"
)

// 00071: the review setup of ADR-112 becomes a profile the Burn follows.
func TestBurnReviewBecomesAProfile(t *testing.T) {
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
	if _, err := p.UpTo(ctx, 70); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO repos (id, name, path, created_at, updated_at) VALUES ('rep_1', 'r', '/tmp/r', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO burn_sessions (id, project_id, created_at, updated_at, review_stages, review_agent_id, review_workflow)
		 VALUES ('brn_x1', 'rep_1', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'issue,result', 'agt_r', 'check')`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := s.Burn().Session(ctx, "rep_1")
	if err != nil || b.ReviewProfileID != "brp_x1" {
		t.Fatalf("session = %+v, %v", b, err)
	}
	prof, err := s.Burn().ReviewProfile(ctx, b.ReviewProfileID)
	if err != nil || len(prof.Stages) != 2 || prof.Stages["issue"].AgentID != "agt_r" || prof.Stages["result"].Workflow != "check" {
		t.Fatalf("profile = %+v, %v", prof, err)
	}
}
