package team_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/team"
)

type installs struct{ keys []string }

func (i *installs) InstallDefaults(_ context.Context, _ string, keys []string) error {
	i.keys = append(i.keys, keys...)
	return nil
}

func setup(t *testing.T) (storage.Store, *team.Service, *installs, storage.Repo) {
	t.Helper()
	st, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, err := st.Repos().Create(context.Background(), storage.Repo{Name: "shop", Path: "/code/shop"})
	if err != nil {
		t.Fatal(err)
	}
	wf := &installs{}
	return st, team.NewService(st, wf), wf, r
}

func TestPacksAreValid(t *testing.T) {
	list, err := team.Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Key != "solo" || list[2].Key != "council" {
		t.Fatalf("packs = %+v", list)
	}
	for _, p := range list {
		if err := team.Validate(p.Agents); err != nil {
			t.Fatalf("%s: %v", p.Key, err)
		}
		if !slices.ContainsFunc(p.Agents, func(a team.AgentSpec) bool { return a.Key == p.Default }) {
			t.Fatalf("%s: default %q is not one of its agents", p.Key, p.Default)
		}
	}
}

func TestApplyPackSaveDeleteRestore(t *testing.T) {
	ctx := context.Background()
	st, svc, wf, r := setup(t)
	p, err := team.PackByKey("council")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyPack(ctx, r.ID, p, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(wf.keys, "hoi-dong-3-ben") {
		t.Fatalf("workflows installed = %v", wf.keys)
	}
	if err := svc.ApplyPack(ctx, r.ID, p, false); !errors.Is(err, team.ErrHasAgents) {
		t.Fatalf("second pack without replace: %v", err)
	}
	agents, _ := st.Agents().List(ctx, r.ID)
	r, _ = st.Repos().Get(ctx, r.ID)
	if d, _ := storage.DefaultAgent(r, agents); d.Key != "planner" || len(agents) != 6 {
		t.Fatalf("default = %s, %d agents", d.Key, len(agents))
	}
	planner := agents[0]
	// a bad key is refused; the default moves on when it is deleted
	if _, err := svc.SaveAgent(ctx, storage.Agent{ProjectID: r.ID, Key: "Bad Key", Name: "x", ModelTier: "fast"}); err == nil {
		t.Fatal("bad key accepted")
	}
	planner.Name = "Kế hoạch"
	if _, err := svc.SaveAgent(ctx, planner); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAgent(ctx, planner.ID); err != nil {
		t.Fatal(err)
	}
	r, _ = st.Repos().Get(ctx, r.ID)
	agents, _ = st.Agents().List(ctx, r.ID)
	if r.DefaultAgentID != agents[0].ID || len(agents) != 5 {
		t.Fatalf("default after delete = %q", r.DefaultAgentID)
	}
	// restore the one before the delete: the planner is back, the others kept their ids
	revs, _ := svc.Revisions(ctx, r.ID, 10)
	if len(revs) != 2 || revs[0].Action != "agent.delete:planner" {
		t.Fatalf("revisions = %+v", revs)
	}
	executor := agents[0]
	if _, err := svc.Restore(ctx, revs[0].ID); err != nil {
		t.Fatal(err)
	}
	agents, _ = st.Agents().List(ctx, r.ID)
	r, _ = st.Repos().Get(ctx, r.ID)
	if len(agents) != 6 || agents[0].Key != "planner" || agents[0].Name != "Kế hoạch" || agents[1].ID != executor.ID {
		t.Fatalf("after restore = %+v", agents)
	}
	if d, _ := storage.DefaultAgent(r, agents); d.Key != "planner" {
		t.Fatalf("default after restore = %s", d.Key)
	}
	// the first agent of an empty project is its default
	r2, _ := st.Repos().Create(ctx, storage.Repo{Name: "b", Path: "/b"})
	a, err := svc.SaveAgent(ctx, storage.Agent{ProjectID: r2.ID, Key: "solo", Name: "Solo", ModelTier: "strong"})
	if err != nil {
		t.Fatal(err)
	}
	if r2, _ = st.Repos().Get(ctx, r2.ID); r2.DefaultAgentID != a.ID {
		t.Fatalf("first agent not default: %q", r2.DefaultAgentID)
	}
}
