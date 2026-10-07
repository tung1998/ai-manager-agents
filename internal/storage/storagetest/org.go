package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

func testProviders(t *testing.T, s storage.Store) {
	ctx := context.Background()
	p := s.Providers()
	a, err := p.Create(ctx, storage.Provider{Name: "Claude API", Kind: storage.ProviderAnthropic, APIKeyEnc: "enc", APIKeyHint: "abcd",
		TierModels: map[string]string{"strong": "m1"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Create(ctx, storage.Provider{Name: "Claude API", Kind: storage.ProviderOpenAI}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("duplicate name err = %v", err)
	}
	b, _ := p.Create(ctx, storage.Provider{Name: "GPT", Kind: storage.ProviderOpenAI, Enabled: true})
	if err := p.SetDefault(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.SetDefault(ctx, "prv_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("SetDefault missing err = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := p.SetStatus(ctx, a.ID, "ok", "3 models", []string{"m1", "m2", "m3"}, now); err != nil {
		t.Fatal(err)
	}
	a.TierModels["fast"] = "m3"
	a.BaseURL = "https://example.test"
	if err := p.Update(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := p.Get(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "ok" || len(got.Models) != 3 || got.TierModels["fast"] != "m3" || got.BaseURL != "https://example.test" ||
		got.CheckedAt == nil || !got.CheckedAt.Equal(now) || got.APIKeyEnc != "enc" {
		t.Fatalf("provider after updates = %+v", got)
	}
	list, _ := p.List(ctx)
	if len(list) != 2 || list[0].ID != b.ID || !list[0].IsDefault || list[1].IsDefault {
		t.Fatalf("List default first = %+v", list)
	}
	if err := p.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
}

func testOrg(t *testing.T, s storage.Store) {
	ctx := context.Background()
	repo, err := s.Repos().Create(ctx, storage.Repo{Name: "shop", Path: "/code/shop"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Repos().Create(ctx, storage.Repo{Name: "dup", Path: "/code/shop"}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("duplicate path err = %v", err)
	}
	prov, _ := s.Providers().Create(ctx, storage.Provider{Name: "P", Kind: storage.ProviderAnthropic})
	lead, err := s.Agents().Create(ctx, storage.Agent{ProjectID: repo.ID, Key: "lead", Name: "Lead", ModelTier: "strong",
		ProviderID: prov.ID, Permissions: storage.Permissions{ReadOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	dev, err := s.Agents().Create(ctx, storage.Agent{ProjectID: repo.ID, Key: "dev", Name: "Dev", ModelTier: "fast", Sort: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Agents().Create(ctx, storage.Agent{ProjectID: repo.ID, Key: "lead", Name: "x", ModelTier: "fast"}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("duplicate agent key err = %v", err)
	}
	agents, _ := s.Agents().List(ctx, repo.ID)
	if len(agents) != 2 || agents[0].Key != "lead" || agents[0].ProjectID != repo.ID || !agents[0].Permissions.ReadOnly {
		t.Fatalf("agents = %+v", agents)
	}
	// the default agent: the project's pick when on, else the first on
	if d, ok := storage.DefaultAgent(repo, agents); !ok || d.ID != lead.ID {
		t.Fatalf("default = %+v", d)
	}
	repo.DefaultAgentID = dev.ID
	if err := s.Repos().Update(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Repos().Get(ctx, repo.ID); r.DefaultAgentID != dev.ID {
		t.Fatalf("default agent not saved: %+v", r)
	}
	// on by default; paused/resumed only by SetEnabled, Update keeps it
	if agents[0].Disabled || agents[1].Disabled {
		t.Fatalf("new agents should be on: %+v", agents)
	}
	if err := s.Agents().SetEnabled(ctx, dev.ID, false); err != nil {
		t.Fatal(err)
	}
	agents, _ = s.Agents().List(ctx, repo.ID)
	if d, _ := storage.DefaultAgent(repo, agents); d.ID != lead.ID {
		t.Fatalf("a paused default falls back to the first on: %+v", d)
	}
	off, _ := s.Agents().Get(ctx, dev.ID)
	off.Name = "Dev 2"
	if err := s.Agents().Update(ctx, off); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.Agents().Get(ctx, dev.ID); !a.Disabled || a.Name != "Dev 2" {
		t.Fatalf("update changed paused state: %+v", a)
	}
	if err := s.Agents().SetEnabled(ctx, dev.ID, true); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.Agents().Get(ctx, dev.ID); a.Disabled {
		t.Fatalf("dev should be on again: %+v", a)
	}
	if err := s.Agents().SetEnabled(ctx, "agt_missing", false); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("SetEnabled missing err = %v", err)
	}
	// deleting the provider must not delete the agent, only unlink it
	if err := s.Providers().Delete(ctx, prov.ID); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.Agents().Get(ctx, lead.ID); a.ProviderID != "" {
		t.Fatalf("provider not unlinked: %+v", a)
	}
	// revisions of the project's agents
	for i := 0; i < 3; i++ {
		if _, err := s.Revisions().Create(ctx, storage.Revision{ProjectID: repo.ID, Action: "agent.update:lead", AgentCount: 2, Snapshot: []byte(`{"agents":[]}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Revisions().Prune(ctx, repo.ID, 2); err != nil {
		t.Fatal(err)
	}
	if revs, _ := s.Revisions().List(ctx, repo.ID, 10); len(revs) != 2 || revs[0].ProjectID != repo.ID {
		t.Fatalf("revisions = %+v", revs)
	}
	// deleting the repo cascades to its agents and revisions
	if err := s.Repos().Delete(ctx, repo.ID); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.Agents().List(ctx, repo.ID); len(a) != 0 {
		t.Fatalf("agents survived: %d", len(a))
	}
	if revs, _ := s.Revisions().List(ctx, repo.ID, 10); len(revs) != 0 {
		t.Fatalf("revisions survived: %d", len(revs))
	}
}

func testTx(t *testing.T, s storage.Store) {
	ctx := context.Background()
	boom := errors.New("boom")
	err := s.InTx(ctx, func(tx storage.Store) error {
		if _, err := tx.Repos().Create(ctx, storage.Repo{Name: "a", Path: "/a"}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx err = %v", err)
	}
	if _, err := s.Repos().GetByPath(ctx, "/a"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("rollback failed: %v", err)
	}
	if err := s.InTx(ctx, func(tx storage.Store) error {
		_, err := tx.Repos().Create(ctx, storage.Repo{Name: "b", Path: "/b"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Repos().GetByPath(ctx, "/b"); err != nil {
		t.Fatalf("commit failed: %v", err)
	}
}

func testHelperProjects(t *testing.T, s storage.Store) {
	ctx := context.Background()
	a, err := s.Repos().Create(ctx, storage.Repo{Name: "Trợ lý máy"})
	if err != nil {
		t.Fatalf("project without path: %v", err)
	}
	if _, err := s.Repos().Create(ctx, storage.Repo{Name: "Trợ lý 2"}); err != nil {
		t.Fatalf("second project without path must be allowed: %v", err)
	}
	if _, err := s.Repos().Create(ctx, storage.Repo{Name: "x", Path: "/p"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Repos().Create(ctx, storage.Repo{Name: "y", Path: "/p"}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("duplicate real path err = %v", err)
	}
	// helper projects keep their agents through the table rebuilds
	ag, err := s.Agents().Create(ctx, storage.Agent{ProjectID: a.ID, Key: "assistant", Name: "Trợ lý", ModelTier: "balanced"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Repos().Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Agents().Get(ctx, ag.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("cascade must still work after rebuild")
	}
}
