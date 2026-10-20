package setup_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/setup"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/team"
)

const aiAnswer = "Đây là đề xuất:\n```json\n" + `{
  "description": "Storefront Nuxt bán hàng, thanh toán Stripe.",
  "reason": "Có frontend, thanh toán và vận hành",
  "confidence": 0.8,
  "default_agent": "team-lead",
  "agents": [
    {"id": "office/team-lead", "reason": "Điều phối nhiều mảng"},
    {"id": "office/frontend-dev", "instructions": "Dùng pnpm, chạy pnpm test trước khi commit.", "level": "edit", "reason": "Quy ước trong CLAUDE.md"},
    {"id": "office/nope", "reason": "không có"},
    {"id": "office/devops", "key": "ops", "name": "Vận hành shop", "reason": "Docker compose"}
  ],
  "workflows": [{"name": "fix-tests", "reason": "Có test"}, {"name": "made-up", "reason": "x"}],
  "automations": [{"name": "pr-review-gate", "reason": "Có PR"}],
  "notes": ["Nên kết nối Stripe read-only"]
}` + "\n```"

func env(t *testing.T, answer string) (*setup.Assistant, storage.Store, *provider.Service, string) {
	dir := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(dir, "o.db"))
	t.Cleanup(func() { st.Close() })
	st.Migrate(context.Background())
	org := team.NewService(st, nil)
	box, _ := secrets.Load(filepath.Join(dir, "k"))
	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotPrompt = body["messages"].([]any)[0].(map[string]any)["content"].(string)
		out, _ := json.Marshal(map[string]any{"model": "claude-opus-5-5", "content": []map[string]string{{"type": "text", "text": answer}},
			"usage": map[string]int{"input_tokens": 900, "output_tokens": 300}})
		w.Write(out)
	}))
	t.Cleanup(srv.Close)
	provs := provider.NewService(st, box, llm.Options{})
	key := "sk-ant-test-key-0000"
	if _, err := provs.Create(context.Background(), provider.Input{Name: "Claude", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	_ = gotPrompt
	return setup.New(st, provs, org), st, provs, srv.URL
}

func TestProposeAndAccept(t *testing.T) {
	a, st, _, _ := env(t, aiAnswer)
	a.Toolbox = func(string) setup.Toolbox {
		return setup.Toolbox{Workflows: []setup.Tool{{Name: "fix-tests"}, {Name: "review-pr"}}}
	}
	ctx := context.Background()
	res, err := a.Propose(ctx, "", "shop", "Project: shop\nFramework: Nuxt", "", []setup.Tool{{Name: "pr-review-gate"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 0 || len(res.Plan.Agents) != 3 || res.Plan.Default != "team-lead" {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Proposal.Workflows) != 1 || res.Proposal.Workflows[0].Name != "fix-tests" || len(res.Proposal.Automations) != 1 || len(res.Warnings) != 2 {
		t.Fatalf("picks = %+v, warnings %v", res.Proposal, res.Warnings)
	}
	fe, ops := res.Plan.Agents[1], res.Plan.Agents[2]
	if !strings.Contains(fe.Instructions, "Bối cảnh project:\nDùng pnpm") || fe.Permissions.Level != "edit" || fe.Permissions.ReadOnly {
		t.Fatalf("frontend = %+v", fe)
	}
	if ops.Key != "ops" || ops.Name != "Vận hành shop" {
		t.Fatalf("ops = %+v", ops)
	}
	if res.Model != "claude-opus-5-5" || res.Usage.InputTokens != 900 {
		t.Fatalf("usage = %+v", res)
	}

	// the project has the starter agent: it stays, and stays the default
	repo, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: "/code/shop"})
	org := team.NewService(st, nil)
	org.AddFromLibrary(ctx, repo.ID, team.StarterAgent)
	picks := append(res.Proposal.Agents[:2:2], setup.AgentPick{ID: "office/assistant", Reason: "trùng"})
	if _, err := a.Accept(ctx, repo.ID, picks, "team-lead", nil); err != nil {
		t.Fatal(err)
	}
	agents, _ := st.Agents().List(ctx, repo.ID)
	keys := map[string]bool{}
	for _, ag := range agents {
		keys[ag.Key] = true
	}
	if r, _ := st.Repos().Get(ctx, repo.ID); len(agents) != 3 || !keys["team-lead"] || !keys["frontend-dev"] || agents[0].Key != "assistant" || r.DefaultAgentID != agents[0].ID {
		t.Fatalf("installed = %v, default %q", keys, r.DefaultAgentID)
	}
}

func TestBuildReportsProblems(t *testing.T) {
	_, problems, _ := setup.Build([]setup.AgentPick{{ID: "office/devops", Key: "Bad Key", Reason: "x"}}, "", nil, nil)
	if len(problems) == 0 {
		t.Fatalf("problems = %v", problems)
	}
	if _, problems, _ := setup.Build(nil, "", nil, nil); len(problems) == 0 {
		t.Fatal("a project with no agent at all must be refused")
	}
}

func TestBuildInvalidLevelIsDroppedWithWarning(t *testing.T) {
	plan, problems, warnings := setup.Build([]setup.AgentPick{
		{ID: "office/devops", Level: ptr("god-mode"), Reason: "x"},
	}, "", nil, nil)
	if len(problems) != 0 || len(warnings) == 0 || plan.Default != "devops" {
		t.Fatalf("problems = %v, warnings = %v, plan %+v", problems, warnings, plan)
	}
	if p := plan.Agents[0].Permissions; p.Level != "operate" {
		t.Fatalf("invalid level must leave the library's: %+v", p)
	}
}

func ptr(s string) *string { return &s }

func TestNoProvider(t *testing.T) {
	dir := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(dir, "o.db"))
	defer st.Close()
	st.Migrate(context.Background())
	box, _ := secrets.Load(filepath.Join(dir, "k"))
	a := setup.New(st, provider.NewService(st, box, llm.Options{}), team.NewService(st, nil))
	if _, err := a.Propose(context.Background(), "", "x", "", "goal", nil); !errors.Is(err, setup.ErrNoProvider) {
		t.Fatalf("err = %v", err)
	}
}

func TestBadJSON(t *testing.T) {
	a, _, _, _ := env(t, "Xin lỗi, tôi không chắc.")
	if _, err := a.Propose(context.Background(), "", "x", "p", "", nil); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("err = %v", err)
	}
}
