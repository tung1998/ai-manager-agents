package assistant_test

import (
	"context"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/assistant"
	"bitbucket.org/senprints/agent-office/internal/orgmodel"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func TestEnsureOnce(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	dir := t.TempDir()
	id, err := assistant.Ensure(ctx, st, org, dir)
	if err != nil || id == "" {
		t.Fatalf("ensure = %q %v", id, err)
	}
	again, err := assistant.Ensure(ctx, st, org, dir)
	if err != nil || again != id {
		t.Fatalf("second ensure = %q %v (want %q)", again, err, id)
	}
	if got := assistant.ID(ctx, st); got != id {
		t.Fatalf("ID = %q", got)
	}
	m, err := st.OrgModels().GetForRepo(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	agents, _ := st.Agents().List(ctx, m.ID)
	if len(agents) != 1 || agents[0].Name != "Trợ lý office" || agents[0].Instructions == "" {
		t.Fatalf("agents = %+v", agents)
	}
	repos, _ := st.Repos().List(ctx)
	if len(repos) != 1 || repos[0].Path != dir {
		t.Fatalf("repos = %+v", repos)
	}
}
