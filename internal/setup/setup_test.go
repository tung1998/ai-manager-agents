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
	"bitbucket.org/senprints/agent-office/internal/scan"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/setup"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/team"
)

const aiAnswer = "Đây là đề xuất:\n```json\n" + `{
  "description": "Storefront Nuxt bán hàng, thanh toán Stripe.",
  "pack_key": "team",
  "reason": "Có frontend, thanh toán và vận hành",
  "confidence": 0.8,
  "agent_changes": [
    {"action": "update", "key": "engineer", "instructions": "Dùng pnpm, chạy pnpm test trước khi commit.", "reason": "Quy ước trong CLAUDE.md"},
    {"action": "add", "key": "graylog-reader", "name": "Graylog Reader", "source": ".claude/skills/fetch-graylog-logs/SKILL.md", "reason": "Project log qua Graylog"},
    {"action": "remove", "key": "monitor", "reason": "Đã có graylog-reader"}
  ],
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
	ctx := context.Background()
	res, err := a.Propose(ctx, "", "shop", "Project: shop\nFramework: Nuxt", &scan.Summary{Tests: []string{"Go"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Proposal.PackKey != "team" || len(res.Proposal.Changes) != 3 || len(res.Problems) != 0 {
		t.Fatalf("result = %+v", res)
	}
	if len(res.SuggestedWorkflows) != 0 { // team pack already has fix-tests
		t.Fatalf("suggested workflows = %v", res.SuggestedWorkflows)
	}
	var eng, reader *team.AgentSpec
	for i, ag := range res.Pack.Agents {
		switch ag.Key {
		case "engineer":
			eng = &res.Pack.Agents[i]
		case "graylog-reader":
			reader = &res.Pack.Agents[i]
		case "monitor":
			t.Fatal("monitor should be removed")
		}
	}
	if eng == nil || !strings.Contains(eng.Instructions, "Bối cảnh project:\nDùng pnpm") || !strings.HasPrefix(eng.Instructions, "Làm đúng task") {
		t.Fatalf("engineer = %+v", eng)
	}
	if reader == nil || !reader.Permissions.ReadOnly || reader.ModelTier != "fast" {
		t.Fatalf("reader = %+v", reader)
	}
	if res.Model != "claude-opus-5-5" || res.Usage.InputTokens != 900 {
		t.Fatalf("usage = %+v", res)
	}

	repo, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: "/code/shop"})
	if _, err := a.Accept(ctx, repo.ID, res.Proposal.PackKey, res.Proposal.Changes[:2]); err != nil { // user unticked "remove monitor"
		t.Fatal(err)
	}
	agents, _ := st.Agents().List(ctx, repo.ID)
	keys := map[string]bool{}
	for _, ag := range agents {
		keys[ag.Key] = true
	}
	if r, _ := st.Repos().Get(ctx, repo.ID); !keys["graylog-reader"] || !keys["monitor"] || r.DefaultAgentID != agents[0].ID || agents[0].Key != "team-lead" {
		t.Fatalf("installed = %v, default %q", keys, r.DefaultAgentID)
	}
}

func TestBuildReportsProblems(t *testing.T) {
	_, problems, err := setup.Build("team", []setup.AgentChange{
		{Action: "add", Key: "Bad Key", Reason: "x"},
	})
	if err != nil || len(problems) == 0 {
		t.Fatalf("problems = %v, %v", problems, err)
	}
	if _, _, err := setup.Build("nope", nil); err == nil {
		t.Fatal("unknown pack must fail")
	}
}

func TestApplyUpdateReadOnlyFalseForcesApproval(t *testing.T) {
	p, problems, err := setup.Build("team", []setup.AgentChange{
		{Action: "update", Key: "product-manager", ReadOnly: boolPtr(false), Reason: "cần ghi file"},
	})
	if err != nil || len(problems) != 0 {
		t.Fatalf("problems = %v, %v", problems, err)
	}
	var found bool
	for _, a := range p.Agents {
		if a.Key != "product-manager" {
			continue
		}
		found = true
		if a.Permissions.ReadOnly {
			t.Fatal("ReadOnly phải là false sau update")
		}
		if !a.Permissions.RequiresApproval {
			t.Fatal("agent ghi được sau update phải RequiresApproval=true")
		}
	}
	if !found {
		t.Fatal("agent product-manager không còn trong pack")
	}
}

func TestApplyUpdateWithoutReadOnlyKeepsApproval(t *testing.T) {
	p, problems, err := setup.Build("team", []setup.AgentChange{
		{Action: "update", Key: "team-lead", Name: "Trưởng nhóm mới", Reason: "đổi tên"},
	})
	if err != nil || len(problems) != 0 {
		t.Fatalf("problems = %v, %v", problems, err)
	}
	for _, a := range p.Agents {
		if a.Key != "team-lead" {
			continue
		}
		if a.Permissions.RequiresApproval {
			t.Fatal("update không đụng ReadOnly thì không được tự bật RequiresApproval")
		}
		return
	}
	t.Fatal("agent team-lead không còn trong pack")
}

func boolPtr(b bool) *bool { return &b }

func TestNoProvider(t *testing.T) {
	dir := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(dir, "o.db"))
	defer st.Close()
	st.Migrate(context.Background())
	box, _ := secrets.Load(filepath.Join(dir, "k"))
	a := setup.New(st, provider.NewService(st, box, llm.Options{}), team.NewService(st, nil))
	if _, err := a.Propose(context.Background(), "", "x", "", nil, "goal"); !errors.Is(err, setup.ErrNoProvider) {
		t.Fatalf("err = %v", err)
	}
}

func TestBadJSON(t *testing.T) {
	a, _, _, _ := env(t, "Xin lỗi, tôi không chắc.")
	if _, err := a.Propose(context.Background(), "", "x", "p", nil, ""); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("err = %v", err)
	}
}

func TestSuggestWorkflows(t *testing.T) {
	has := func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	t.Run("go tests suggest fix-tests", func(t *testing.T) {
		got := setup.SuggestWorkflows(&scan.Summary{Tests: []string{"Go"}}, nil)
		if !has(got, "fix-tests") {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("js tests suggest fix-tests", func(t *testing.T) {
		got := setup.SuggestWorkflows(&scan.Summary{Tests: []string{"JS/TS"}}, nil)
		if !has(got, "fix-tests") {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("php tests suggest fix-tests", func(t *testing.T) {
		got := setup.SuggestWorkflows(&scan.Summary{Tests: []string{"PHP"}}, nil)
		if !has(got, "fix-tests") {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("github actions suggests review-pr", func(t *testing.T) {
		got := setup.SuggestWorkflows(&scan.Summary{Infra: []string{"GitHub Actions"}}, nil)
		if !has(got, "review-pr") {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("no signals, no suggestion", func(t *testing.T) {
		got := setup.SuggestWorkflows(&scan.Summary{}, nil)
		if len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("already in pack, not suggested again", func(t *testing.T) {
		got := setup.SuggestWorkflows(&scan.Summary{Tests: []string{"Go"}}, []string{"fix-tests"})
		if has(got, "fix-tests") {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("nil summary, no suggestion", func(t *testing.T) {
		got := setup.SuggestWorkflows(nil, nil)
		if len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})
}

func TestBuildMergesExtraWorkflows(t *testing.T) {
	t.Run("adds a new workflow key", func(t *testing.T) {
		p, _, err := setup.Build("solo", nil, "fix-tests")
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Workflows) != 1 || p.Workflows[0] != "fix-tests" {
			t.Fatalf("workflows = %v", p.Workflows)
		}
	})
	t.Run("untick: not passed, not added", func(t *testing.T) {
		p, _, err := setup.Build("solo", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Workflows) != 0 {
			t.Fatalf("workflows = %v", p.Workflows)
		}
	})
	t.Run("no duplicate when pack already has it", func(t *testing.T) {
		p, _, err := setup.Build("team", nil, "fix-tests")
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, w := range p.Workflows {
			if w == "fix-tests" {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("fix-tests count = %d in %v", n, p.Workflows)
		}
	})
}
