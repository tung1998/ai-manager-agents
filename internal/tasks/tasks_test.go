package tasks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/orgmodel"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/tasks"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// fakeModel answers by what is asked; votes follow voteFor(agent system prompt).
type fakeModel struct {
	mu      sync.Mutex
	voteFor func(system string) string
	block   bool
	planFor string // council plan assignee
}

func (f *fakeModel) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			System   string `json:"system"`
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		var prompt string
		json.Unmarshal(body.Messages[len(body.Messages)-1].Content, &prompt)
		var text string
		switch {
		case strings.Contains(prompt, "Lập kế hoạch:"):
			if strings.Contains(body.System, "Trưởng nhóm") {
				text = "Kế hoạch.\n```json\n" + `{"analysis":"cần đọc code và sửa","assignments":[{"agent":"code-reader","task":"đọc a.txt"},{"agent":"engineer","task":"đổi one thành ONE"},{"agent":"ghost","task":"x"}]}` + "\n```"
			} else {
				text = "```json\n" + `{"analysis":"giao worker","assignments":[{"agent":"code-worker","task":"đổi one thành ONE"}]}` + "\n```"
			}
		case strings.Contains(prompt, "Hội đồng đang xét"):
			v := f.voteFor(body.System)
			text = "```json\n{\"vote\":\"" + v + "\",\"reason\":\"lý do " + v + "\"}\n```"
		case strings.Contains(prompt, "Bạn là giám sát"):
			b := "false"
			if f.block {
				b = "true"
			}
			text = "Đã kiểm tra.\n```json\n{\"verdict\":\"fail\",\"summary\":\"diff sai phạm vi\",\"issues\":[\"x\"],\"block_changes\":" + b + "}\n```"
		case strings.Contains(prompt, "giao cho bạn một việc"):
			if strings.Contains(prompt, "đổi one thành ONE") {
				text = "Đề xuất:\n```diff\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-one\n+ONE\n```"
			} else {
				text = "a.txt có dòng one (a.txt:1)."
			}
		case strings.Contains(prompt, "Viết câu trả lời cuối"):
			text = "## Kết luận\nĐã có đề xuất đổi one thành ONE."
		default:
			text = "?"
		}
		out, _ := json.Marshal(map[string]any{"model": "claude-sonnet-5", "stop_reason": "end_turn",
			"content": []map[string]any{{"type": "text", "text": text}}, "usage": map[string]int{"input_tokens": 1000, "output_tokens": 100}})
		w.Write(out)
	}
}

type fixture struct {
	st      storage.Store
	svc     *tasks.Service
	project storage.Repo
	dir     string
}

func setup(t *testing.T, fm *fakeModel, templateKey string) fixture {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	t.Cleanup(func() { st.Close() })
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	provs := provider.NewService(st, box, llm.Options{})
	u := usage.New(st, time.UTC)
	provs.SetUsage(u)
	srv := httptest.NewServer(fm.handler(t))
	t.Cleanup(srv.Close)
	key := "sk-ant-test-key-0000"
	provs.Create(ctx, provider.Input{Name: "C", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: &key,
		TierModels: map[string]string{"strong": "claude-sonnet-5", "balanced": "claude-sonnet-5", "fast": "claude-sonnet-5"}})
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "demo", Path: dir})
	tpl, _ := st.OrgModels().GetTemplateByKey(ctx, templateKey)
	org.ApplyToRepo(ctx, project.ID, tpl.ID, false)
	return fixture{st: st, svc: tasks.New(st, chat.NewEngine(st, provs, u)), project: project, dir: dir}
}

func wait(t *testing.T, svc *tasks.Service, id string) tasks.Detail {
	t.Helper()
	l, ok := svc.Live(id)
	if !ok {
		t.Fatal("no live stream")
	}
	deadline := time.After(20 * time.Second)
	for {
		_, done, wake := l.Since(0)
		if done {
			break
		}
		select {
		case <-wake:
		case <-deadline:
			t.Fatal("task did not finish")
		}
	}
	d, err := svc.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func phases(d tasks.Detail) string {
	var p []string
	for _, s := range d.Steps {
		p = append(p, s.Phase+":"+s.AgentKey)
	}
	return strings.Join(p, ",")
}

func requireGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func TestTeamHierarchy(t *testing.T) {
	requireGit(t)
	f := setup(t, &fakeModel{}, "team")
	task, err := f.svc.Start(context.Background(), f.project.ID, "Đổi one thành ONE trong a.txt", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Start(context.Background(), f.project.ID, "việc khác", 0, nil); err != tasks.ErrBusy {
		t.Fatalf("second task err = %v", err)
	}
	d := wait(t, f.svc, task.ID)
	got := phases(d)
	if d.Task.Status != "done" || !strings.HasPrefix(got, "plan:team-lead,") || !strings.HasSuffix(got, ",synthesize:team-lead") ||
		!strings.Contains(got, "work:code-reader") || !strings.Contains(got, "work:engineer") || strings.Contains(got, "ghost") {
		t.Fatalf("status=%s phases=%s detail=%s", d.Task.Status, got, d.Task.Detail)
	}
	if !strings.Contains(d.Task.Result, "Kết luận") || d.Task.CostUSD <= 0 {
		t.Fatalf("result=%q cost=%v", d.Task.Result, d.Task.CostUSD)
	}
	if len(d.Patches) != 1 || d.Patches[0].Status != "pending" {
		t.Fatalf("patches = %+v", d.Patches)
	}
	if plan := d.Steps[0]; plan.Data["assignments"] == nil || strings.Contains(plan.Output, "```json") {
		t.Fatalf("plan step = %+v", plan)
	}
	runs, _ := f.st.Runs().List(context.Background(), storage.RunFilter{Limit: 20})
	if len(runs) != 4 || runs[0].Kind != "task" {
		t.Fatalf("runs = %d %+v", len(runs), runs[0])
	}
}

func TestCouncilApprovedAuditorBlocks(t *testing.T) {
	requireGit(t)
	fm := &fakeModel{voteFor: func(string) string { return "approve" }, block: true}
	f := setup(t, fm, "council")
	task, _ := f.svc.Start(context.Background(), f.project.ID, "Đổi one thành ONE", 0, nil)
	d := wait(t, f.svc, task.ID)
	got := phases(d)
	want := "plan:planner,vote:executor,vote:auditor,work:code-worker,review:auditor,synthesize:executor"
	if got != want || d.Task.Status != "done" {
		t.Fatalf("status=%s phases=%s want %s (%s)", d.Task.Status, got, want, d.Task.Detail)
	}
	if len(d.Patches) != 1 || d.Patches[0].Status != "rejected" || !strings.Contains(d.Patches[0].Detail, "phủ quyết") {
		t.Fatalf("auditor veto did not reject the patch: %+v", d.Patches)
	}
	for _, s := range d.Steps {
		if s.Phase == "vote" && s.Data["vote"] != "approve" {
			t.Fatalf("vote data = %+v", s.Data)
		}
	}
}

func TestCouncilRejected(t *testing.T) {
	fm := &fakeModel{voteFor: func(system string) string {
		if strings.Contains(system, "Giám sát") {
			return "reject" // the veto holder says no
		}
		return "approve"
	}}
	f := setup(t, fm, "council")
	task, _ := f.svc.Start(context.Background(), f.project.ID, "Việc rủi ro", 0, nil)
	d := wait(t, f.svc, task.ID)
	got := phases(d)
	if d.Task.Status != "rejected" || got != "plan:planner,vote:executor,vote:auditor,revise:planner,vote:executor,vote:auditor" {
		t.Fatalf("status=%s phases=%s", d.Task.Status, got)
	}
	if !strings.Contains(d.Task.Result, "phủ quyết") {
		t.Fatalf("result = %q", d.Task.Result)
	}
}

func TestTaskBudget(t *testing.T) {
	f := setup(t, &fakeModel{}, "team")
	// one call costs 1000*2/1e6 + 100*10/1e6 = 0.003
	task, _ := f.svc.Start(context.Background(), f.project.ID, "x", 0.001, nil)
	d := wait(t, f.svc, task.ID)
	if d.Task.Status != "failed" || !strings.Contains(d.Task.Detail, "ngân sách") || len(d.Steps) != 1 {
		t.Fatalf("status=%s detail=%q steps=%d", d.Task.Status, d.Task.Detail, len(d.Steps))
	}
}
