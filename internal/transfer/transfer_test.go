package transfer_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/team"
	"bitbucket.org/senprints/agent-office/internal/transfer"
	"bitbucket.org/senprints/agent-office/internal/workflow"
)

type office struct {
	st   storage.Store
	org  *team.Service
	wf   *workflow.Service
	prov *provider.Service
	tr   *transfer.Service
}

func newOffice(t *testing.T) office {
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.Migrate(context.Background())
	box, _ := secrets.Load(filepath.Join(dir, "k"))
	lib := workflow.Library{Dir: filepath.Join(dir, "workflows")}
	lib.Seed()
	wf := &workflow.Service{Store: st, Lib: lib}
	org := team.NewService(st, wf)
	prov := provider.NewService(st, box, llm.Options{})
	return office{st, org, wf, prov, transfer.New(st, prov, org, lib)}
}

func TestExportImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := newOffice(t)
	key := "sk-ant-secret-key-9999"
	claude, _ := src.prov.Create(ctx, provider.Input{Name: "Claude API", Kind: storage.ProviderAnthropic, APIKey: &key})
	gpt, _ := src.prov.Create(ctx, provider.Input{Name: "GPT", Kind: storage.ProviderOpenAI, APIKeyEnv: "OPENAI_API_KEY"})
	pack, _ := team.PackByKey("team")
	repo, _ := src.st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: "/code/shop", Description: "Shop"})
	if err := src.org.ApplyPack(ctx, repo.ID, pack, false); err != nil {
		t.Fatal(err)
	}
	agents, _ := src.st.Agents().List(ctx, repo.ID)
	old := agents[0]
	agents[0].ProviderID = gpt.ID
	agents[0].Name = "Trưởng nhóm shop"
	src.org.SaveAgent(ctx, agents[0], old)
	src.st.Repos().Create(ctx, storage.Repo{Name: "Trợ lý máy"})
	mine := strings.Replace(workflow.BuiltinSource("handoff"), "key: handoff", "key: giao-gon", 1)
	if _, err := src.wf.Lib.Save("giao-gon", mine); err != nil {
		t.Fatal(err)
	}
	_ = claude

	b, err := src.tr.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := transfer.WriteDir(dir, b); err != nil {
		t.Fatal(err)
	}
	all := ""
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			raw, _ := os.ReadFile(p)
			all += string(raw)
		}
		return nil
	})
	if strings.Contains(all, "secret-key") || strings.Contains(all, "api_key_enc") || strings.Contains(all, gpt.ID) {
		t.Fatal("export leaked a secret or an internal id")
	}

	read, err := transfer.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	dst := newOffice(t)
	plan, err := dst.tr.Import(ctx, read, true)
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]string{}
	for _, c := range plan.Changes {
		ops[c.Kind+":"+c.Name] = c.Op
	}
	if ops["provider:Claude API"] != "skip" || ops["provider:GPT"] != "create" || ops["workflow:giao-gon"] != "create" || ops["project:shop (/code/shop)"] != "create" || ops["project:Trợ lý máy"] != "create" {
		t.Fatalf("plan = %v", ops)
	}
	if list, _ := dst.st.Repos().List(ctx); len(list) != 0 {
		t.Fatal("dry run wrote data")
	}

	if _, err := dst.tr.Import(ctx, read, false); err != nil {
		t.Fatal(err)
	}
	r, err := dst.st.Repos().GetByPath(ctx, "/code/shop")
	if err != nil {
		t.Fatal(err)
	}
	dstAgents, _ := dst.st.Agents().List(ctx, r.ID)
	dstGPT := findProvider(t, dst.st, "GPT")
	if dstAgents[0].Name != "Trưởng nhóm shop" || dstAgents[0].ProviderID != dstGPT.ID || r.DefaultAgentID != dstAgents[0].ID {
		t.Fatalf("imported agents = %+v, default %q", dstAgents[0], r.DefaultAgentID)
	}
	wfs, _ := dst.st.Workflows().List(ctx, r.ID)
	srcWfs, _ := src.st.Workflows().List(ctx, repo.ID)
	if len(wfs) != len(srcWfs) || len(wfs) == 0 || wfs[0].SourceKey == "" || len(wfs[0].Bindings) != len(srcWfs[0].Bindings) {
		t.Fatalf("imported workflows = %+v", wfs)
	}
	if _, err := dst.wf.Lib.Get("giao-gon"); err != nil {
		t.Fatal(err)
	}

	// importing the same bundle again changes nothing
	again, _ := dst.tr.Import(ctx, read, false)
	for _, c := range again.Changes {
		if c.Op != "unchanged" && c.Op != "skip" {
			t.Fatalf("second import changed %s %s: %s", c.Kind, c.Name, c.Op)
		}
	}

	// removing a project from the bundle removes its file on the next export
	vi, _ := filepath.Glob(filepath.Join(dir, "projects", "tro-ly-may-*.json"))
	if len(vi) != 1 {
		t.Fatal("vietnamese project name should become an ascii slug")
	}
	b.Projects = b.Projects[:1]
	transfer.WriteDir(dir, b)
	files, _ := filepath.Glob(filepath.Join(dir, "projects", "*.json"))
	if len(files) != 1 {
		t.Fatalf("stale project files: %v", files)
	}
}

// A dry-run preview must agree with what the real import does. Providers
// created by the same bundle must be visible to the agents being resolved in
// the same pass, not fall back to the default-provider placeholder.
func TestImportDryRunMatchesRealRunForNewProvider(t *testing.T) {
	ctx := context.Background()
	run := func(t *testing.T, bundle transfer.Bundle) string {
		dst := newOffice(t)
		repo, _ := dst.st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: "/code/shop"})
		agent := storage.Agent{ProjectID: repo.ID, Key: "lead", Name: "Lead", ModelTier: "balanced"}
		created, err := dst.st.Agents().Create(ctx, agent)
		if err != nil {
			t.Fatal(err)
		}
		if created.ProviderID != "" {
			t.Fatalf("test setup: agent should default to no provider, got %q", created.ProviderID)
		}
		if err := dst.st.Repos().SetDefaultAgent(ctx, repo.ID, created.ID); err != nil {
			t.Fatal(err)
		}
		bundle.Projects[0].Path = "/code/shop"

		plan, err := dst.tr.Import(ctx, bundle, true)
		if err != nil {
			t.Fatal(err)
		}
		dryOp := ""
		for _, c := range plan.Changes {
			if c.Kind == "project" {
				dryOp = c.Op
			}
		}

		real := newOffice(t)
		repo2, _ := real.st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: "/code/shop"})
		agent2 := agent
		agent2.ProjectID = repo2.ID
		created2, err := real.st.Agents().Create(ctx, agent2)
		if err != nil {
			t.Fatal(err)
		}
		if err := real.st.Repos().SetDefaultAgent(ctx, repo2.ID, created2.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := real.tr.Import(ctx, bundle, false); err != nil {
			t.Fatal(err)
		}
		realPlan, err := real.tr.Import(ctx, bundle, true)
		if err != nil {
			t.Fatal(err)
		}
		realOp := ""
		for _, c := range realPlan.Changes {
			if c.Kind == "project" {
				realOp = c.Op
			}
		}
		if realOp != "unchanged" {
			t.Fatalf("real import did not settle: %s", realOp)
		}
		return dryOp
	}

	t.Run("agent references new provider", func(t *testing.T) {
		bundle := transfer.Bundle{Version: transfer.Version, Providers: []transfer.ProviderSpec{
			{Name: "New Provider", Kind: string(storage.ProviderClaudeCLI), Enabled: true},
		}, Projects: []transfer.ProjectSpec{{
			Name: "shop",
			Agents: &team.Snapshot{Default: "lead", Agents: []team.AgentSpec{
				{Key: "lead", Name: "Lead", ModelTier: "balanced", Provider: "New Provider"},
			}},
		}}}
		if op := run(t, bundle); op != "update" {
			t.Fatalf("dry-run project op = %q, want update", op)
		}
	})

	t.Run("fallback references new provider", func(t *testing.T) {
		bundle := transfer.Bundle{Version: transfer.Version, Providers: []transfer.ProviderSpec{
			{Name: "New Provider", Kind: string(storage.ProviderClaudeCLI), Enabled: true},
		}, Projects: []transfer.ProjectSpec{{
			Name: "shop",
			Agents: &team.Snapshot{Default: "lead", Agents: []team.AgentSpec{
				{Key: "lead", Name: "Lead", ModelTier: "balanced", FallbackNames: []string{"New Provider|"}},
			}},
		}}}
		if op := run(t, bundle); op != "update" {
			t.Fatalf("dry-run project op = %q, want update", op)
		}
	})

	t.Run("provider skipped for missing key stays unchanged", func(t *testing.T) {
		bundle := transfer.Bundle{Version: transfer.Version, Providers: []transfer.ProviderSpec{
			{Name: "New Anthropic", Kind: string(storage.ProviderAnthropic), Enabled: true},
		}, Projects: []transfer.ProjectSpec{{
			Name: "shop",
			Agents: &team.Snapshot{Default: "lead", Agents: []team.AgentSpec{
				{Key: "lead", Name: "Lead", ModelTier: "balanced", Provider: "New Anthropic"},
			}},
		}}}
		if op := run(t, bundle); op != "unchanged" {
			t.Fatalf("dry-run project op = %q, want unchanged (provider skipped for missing key both times)", op)
		}
	})
}

// storeInterceptingGetByPath wraps a storage.Store so that the repo lookup
// Import's findProject performs (RepoRepo.GetByPath) triggers a side effect
// first, simulating a concurrent SetDefaultAgent landing between Import's
// read of the project and Import's own write.
type storeInterceptingGetByPath struct {
	storage.Store
	onGetByPath func()
}

func (s storeInterceptingGetByPath) Repos() storage.RepoRepo {
	return repoRepoIntercepting{RepoRepo: s.Store.Repos(), onGetByPath: s.onGetByPath}
}

type repoRepoIntercepting struct {
	storage.RepoRepo
	onGetByPath func()
}

func (r repoRepoIntercepting) GetByPath(ctx context.Context, path string) (storage.Repo, error) {
	repo, err := r.RepoRepo.GetByPath(ctx, path)
	if err == nil && r.onGetByPath != nil {
		r.onGetByPath()
	}
	return repo, err
}

// a project workflow whose bundle key does not match the key in its own
// source must be skipped, not re-"created" forever.
func TestImportMismatchedWorkflowKeySkipped(t *testing.T) {
	ctx := context.Background()
	dst := newOffice(t)
	repo, _ := dst.st.Repos().Create(ctx, storage.Repo{Name: "shop"})
	_ = repo

	source := strings.Replace(workflow.BuiltinSource("handoff"), "key: handoff", "key: bar", 1)
	b := transfer.Bundle{
		Version:   transfer.Version,
		Providers: []transfer.ProviderSpec{},
		Workflows: []transfer.LibraryEntry{},
		Projects: []transfer.ProjectSpec{{
			Name:      "shop",
			Workflows: []transfer.ProjectWorkflow{{Key: "foo", Source: source, Enabled: true}},
		}},
	}

	plan, err := dst.tr.Import(ctx, b, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Op != "skip" {
		t.Fatalf("first dry run = %+v, want a single skip", plan.Changes)
	}

	again, err := dst.tr.Import(ctx, b, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Changes) != 1 || again.Changes[0].Op != "skip" {
		t.Fatalf("second dry run = %+v, want skip again (not create)", again.Changes)
	}
}

func findProvider(t *testing.T, st storage.Store, name string) storage.Provider {
	list, _ := st.Providers().List(context.Background())
	for _, p := range list {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("provider %s missing", name)
	return storage.Provider{}
}
