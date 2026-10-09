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
	if !slices.Contains(wf.keys, "council-3") {
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

func TestMergeKeepsExistingAddsNew(t *testing.T) {
	ctx := context.Background()
	st, svc, _, r := setup(t)
	solo, err := team.PackByKey("solo")
	if err != nil {
		t.Fatal(err)
	}

	// (a) an empty project gets the pack as-is
	if err := svc.Merge(ctx, r.ID, team.Snapshot{Default: solo.Default, Agents: solo.Agents}, "pack:solo"); err != nil {
		t.Fatal(err)
	}
	agents, _ := st.Agents().List(ctx, r.ID)
	if len(agents) != len(solo.Agents) {
		t.Fatalf("empty project merge = %d agents, want %d", len(agents), len(solo.Agents))
	}

	// hand-edit the existing agent, and add one the pack doesn't know about
	existing := agents[0]
	existing.Instructions = "custom instructions"
	existing.Permissions.ReadOnly = !existing.Permissions.ReadOnly
	if _, err := svc.SaveAgent(ctx, existing); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveAgent(ctx, storage.Agent{ProjectID: r.ID, Key: "custom", Name: "Custom", ModelTier: "fast"}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Agents().List(ctx, r.ID)

	// (b)(c)(d): apply a bigger pack; existing keys untouched, custom kept, new keys added
	council, err := team.PackByKey("council")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Merge(ctx, r.ID, team.Snapshot{Default: council.Default, Agents: council.Agents}, "pack:council"); err != nil {
		t.Fatal(err)
	}
	after, _ := st.Agents().List(ctx, r.ID)
	byKey := map[string]storage.Agent{}
	for _, a := range after {
		byKey[a.Key] = a
	}
	for _, a := range before {
		got, ok := byKey[a.Key]
		if !ok {
			t.Fatalf("existing agent %q removed by merge", a.Key)
		}
		if got.Instructions != a.Instructions || got.Permissions.ReadOnly != a.Permissions.ReadOnly || got.Name != a.Name {
			t.Fatalf("merge changed existing agent %q: %+v -> %+v", a.Key, a, got)
		}
	}
	beforeKeys := map[string]bool{}
	for _, a := range before {
		beforeKeys[a.Key] = true
	}
	added := 0
	for _, spec := range council.Agents {
		if beforeKeys[spec.Key] {
			continue
		}
		added++
		if _, ok := byKey[spec.Key]; !ok {
			t.Fatalf("new pack agent %q not added by merge", spec.Key)
		}
	}
	if len(after) != len(before)+added {
		t.Fatalf("after merge = %d agents, want %d", len(after), len(before)+added)
	}
}
