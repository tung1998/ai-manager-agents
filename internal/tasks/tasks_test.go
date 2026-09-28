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
	"bitbucket.org/senprints/agent-office/internal/worktree"
)

// fakeModel answers by what is asked; votes follow voteFor(agent system prompt).
type fakeModel struct {
	mu      sync.Mutex
	voteFor func(system string) string
	block   bool
	planFor string   // council plan assignee
	reviews []string // successive auditor verdict JSONs (after these: the default fail)
	reviewN int
	edit    bool // workers edit files with tools (worktree) instead of writing diffs
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
		isToolResult := json.Unmarshal(body.Messages[len(body.Messages)-1].Content, &prompt) != nil
		var text string
		switch {
		case isToolResult:
			text = "Đã sửa a.txt."
		case f.edit && strings.Contains(prompt, "giao cho bạn một việc") && strings.Contains(prompt, "đổi one thành ONE"):
			// edits its file, and one it was not given
			out, _ := json.Marshal(map[string]any{"model": "claude-sonnet-5", "stop_reason": "tool_use", "usage": map[string]int{"input_tokens": 100, "output_tokens": 10},
				"content": []map[string]any{
					{"type": "tool_use", "id": "t1", "name": "edit_file", "input": map[string]any{"path": "a.txt", "old": "one", "new": "ONE"}},
					{"type": "tool_use", "id": "t2", "name": "write_file", "input": map[string]any{"path": "stray.txt", "content": "x\n"}},
				}})
			w.Write(out)
			return
		case strings.Contains(prompt, "Lập kế hoạch:"):
			if strings.Contains(body.System, "Trưởng nhóm") {
				text = "Kế hoạch.\n```json\n" + `{"analysis":"cần đọc code và sửa","conventions":"viết hoa toàn bộ","assignments":[{"agent":"code-reader","task":"đọc a.txt"},{"agent":"engineer","task":"đổi one thành ONE","files":["a.txt"],"depends_on":[1]},{"agent":"ghost","task":"x"}]}` + "\n```"
			} else {
				text = "```json\n" + `{"analysis":"giao worker","assignments":[{"agent":"code-worker","task":"đổi one thành ONE","files":["a.txt"]}]}` + "\n```"
			}
		case strings.Contains(prompt, "Hội đồng đang xét"):
			v := f.voteFor(body.System)
			text = "```json\n{\"vote\":\"" + v + "\",\"reason\":\"lý do " + v + "\"}\n```"
		case strings.Contains(prompt, "Bạn là giám sát") && f.nextReview() != "":
			f.mu.Lock()
			text = "Đã kiểm tra.\n```json\n" + f.reviews[f.reviewN] + "\n```"
			f.reviewN++
			f.mu.Unlock()
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

func (f *fakeModel) nextReview() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reviewN < len(f.reviews) {
		return f.reviews[f.reviewN]
	}
	return ""
}

type fixture struct {
	st      storage.Store
	engine  *chat.Engine
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
	engine := chat.NewEngine(st, provs, u)
	return fixture{st: st, engine: engine, svc: tasks.New(st, engine), project: project, dir: dir}
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
	task, err := f.svc.Start(context.Background(), f.project.ID, "Đổi one thành ONE trong a.txt", 0, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Start(context.Background(), f.project.ID, "việc khác", 0, nil, ""); err != tasks.ErrBusy {
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
	task, _ := f.svc.Start(context.Background(), f.project.ID, "Đổi one thành ONE", 0, nil, "")
	d := wait(t, f.svc, task.ID)
	got := phases(d)
	want := "plan:planner,vote:executor,vote:auditor,work:code-worker,review:auditor,synthesize:executor"
	// a failed review is not "done": the task reports why
	if got != want || d.Task.Status != "failed" || !strings.Contains(d.Task.Detail, "chưa đạt") {
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
	task, _ := f.svc.Start(context.Background(), f.project.ID, "Việc rủi ro", 0, nil, "")
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
	task, _ := f.svc.Start(context.Background(), f.project.ID, "x", 0.001, nil, "")
	d := wait(t, f.svc, task.ID)
	if d.Task.Status != "failed" || !strings.Contains(d.Task.Detail, "ngân sách") || len(d.Steps) != 1 {
		t.Fatalf("status=%s detail=%q steps=%d", d.Task.Status, d.Task.Detail, len(d.Steps))
	}
}

func TestRetryWithLessons(t *testing.T) {
	requireGit(t)
	fm := &fakeModel{voteFor: func(string) string { return "approve" }, block: true}
	f := setup(t, fm, "council")
	first, _ := f.svc.Start(context.Background(), f.project.ID, "Đổi one thành ONE", 0.5, nil, "")
	d := wait(t, f.svc, first.ID)
	if d.Task.Status != "failed" {
		t.Fatalf("first run: %s", d.Task.Status)
	}
	again, err := f.svc.Retry(context.Background(), first.ID, true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	d2 := wait(t, f.svc, again.ID)
	if d2.Task.Goal != d.Task.Goal || d2.Task.BudgetUSD != 0.5 || again.ID == first.ID {
		t.Fatalf("retry must keep goal and budget: %+v", d2.Task)
	}
	planned := false
	for _, s := range d2.Steps {
		if s.Phase == "plan" {
			planned = true
		}
	}
	if !planned {
		t.Fatal("retry did not run")
	}
}

func TestCouncilRepairsUntilPass(t *testing.T) {
	requireGit(t)
	fm := &fakeModel{voteFor: func(string) string { return "approve" }, reviews: []string{
		`{"verdict":"fix","summary":"còn lỗi","fixes":[{"job":1,"issue":"gọi hàm không tồn tại"}]}`,
		`{"verdict":"pass","summary":"đạt"}`,
	}}
	f := setup(t, fm, "council")
	task, _ := f.svc.Start(context.Background(), f.project.ID, "Đổi one thành ONE", 0, nil, "")
	d := wait(t, f.svc, task.ID)
	if d.Task.Status != "done" {
		t.Fatalf("status=%s (%s)", d.Task.Status, d.Task.Detail)
	}
	reviews, works := 0, 0
	for _, s := range d.Steps {
		switch s.Phase {
		case "review":
			reviews++
		case "work":
			works++
		}
	}
	if reviews != 2 || works != 2 {
		t.Fatalf("want review→fix→review: reviews=%d works=%d (%s)", reviews, works, phases(d))
	}
	if len(d.Patches) != 2 || d.Patches[0].Status != "rejected" || !strings.Contains(d.Patches[0].Detail, "bản sửa") || d.Patches[1].Status != "pending" {
		t.Fatalf("patches: %+v", d.Patches)
	}
}

func TestCouncilAsksThePerson(t *testing.T) {
	requireGit(t)
	fm := &fakeModel{voteFor: func(string) string { return "approve" }, reviews: []string{
		`{"verdict":"ask","summary":"chưa rõ","question":"Dùng chữ hoa hay chữ thường?"}`,
		`{"verdict":"pass","summary":"đạt"}`,
	}}
	f := setup(t, fm, "council")
	task, _ := f.svc.Start(context.Background(), f.project.ID, "Đổi one thành ONE", 0, nil, "")
	d := wait(t, f.svc, task.ID)
	if d.Task.Status != "needs_input" || d.Task.Detail != "Dùng chữ hoa hay chữ thường?" {
		t.Fatalf("status=%s detail=%s", d.Task.Status, d.Task.Detail)
	}
	again, err := f.svc.Retry(context.Background(), task.ID, false, "", "chữ hoa")
	if err != nil {
		t.Fatal(err)
	}
	if d2 := wait(t, f.svc, again.ID); d2.Task.Status != "done" {
		t.Fatalf("after answer: %s (%s)", d2.Task.Status, d2.Task.Detail)
	}
}

func TestCouncilStopsWhenStuck(t *testing.T) {
	requireGit(t)
	same := `{"verdict":"fix","summary":"vẫn lỗi","fixes":[{"job":1,"issue":"lỗi cũ"}]}`
	fm := &fakeModel{voteFor: func(string) string { return "approve" }, reviews: []string{same, same, same, same, same}}
	f := setup(t, fm, "council")
	task, _ := f.svc.Start(context.Background(), f.project.ID, "Đổi one thành ONE", 0, nil, "")
	d := wait(t, f.svc, task.ID)
	if d.Task.Status != "failed" || !strings.Contains(d.Task.Detail, "vòng sửa") {
		t.Fatalf("status=%s detail=%s", d.Task.Status, d.Task.Detail)
	}
}

func TestTeamWorksInWorktree(t *testing.T) {
	requireGit(t)
	f := setup(t, &fakeModel{edit: true}, "team")
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = f.dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	trees := worktree.New(filepath.Join(t.TempDir(), "wt"))
	f.engine.SetWorktrees(trees)
	task, err := f.svc.Start(context.Background(), f.project.ID, "Đổi one thành ONE trong a.txt", 0, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	d := wait(t, f.svc, task.ID)
	var pending, stray []chat.PatchDTO
	for _, p := range d.Patches {
		switch {
		case p.Status == "pending" && strings.Join(p.Files, ",") == "a.txt":
			pending = append(pending, p)
		case strings.Contains(p.Detail, "ngoài phạm vi") && strings.Join(p.Files, ",") == "stray.txt":
			stray = append(stray, p)
		}
	}
	if d.Task.Status != "done" || len(pending) != 1 || !strings.Contains(pending[0].Diff, "+ONE") || len(stray) == 0 {
		t.Fatalf("status=%s detail=%s patches=%+v", d.Task.Status, d.Task.Detail, d.Patches)
	}
	// the project is untouched until approved; the task's worktree is gone
	if b, _ := os.ReadFile(filepath.Join(f.dir, "a.txt")); string(b) != "one\n" {
		t.Fatalf("a.txt = %q", b)
	}
	if trees.Exists(f.project.ID, chat.TaskTree(task.ID)) {
		t.Fatal("task worktree not removed")
	}
	if _, err := f.engine.ApproveTaskPatches(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.dir, "a.txt")); string(b) != "ONE\n" {
		t.Fatalf("a.txt after approve = %q", b)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "stray.txt")); !os.IsNotExist(err) {
		t.Fatal("stray file reached the project")
	}
}
