package chat_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/officetools"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/workflow"
)

// wfGroup is a group chat with a third agent (QA) and the shipped workflows
// in a library.
type wfGroup struct {
	group
	qa  storage.Agent
	svc *workflow.Service
}

func newWFGroup(t *testing.T) wfGroup {
	t.Helper()
	g := newGroup(t)
	qa, err := g.f.st.Agents().Create(g.context, storage.Agent{ProjectID: g.f.project.ID, Key: "qa", Name: "QA", ModelTier: "fast"})
	if err != nil {
		t.Fatal(err)
	}
	lib := workflow.Library{Dir: filepath.Join(t.TempDir(), "workflows")}
	if _, err := lib.Seed(); err != nil {
		t.Fatal(err)
	}
	return wfGroup{group: g, qa: qa, svc: &workflow.Service{Store: g.f.st, Lib: lib}}
}

func (g wfGroup) install(t *testing.T, key string, bindings map[string]string) storage.Workflow {
	t.Helper()
	w, err := g.svc.Install(g.context, g.f.project.ID, key, bindings)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// during calls a workflow tool from inside the coordinator's answer in progress.
func (g wfGroup) during(t *testing.T, name string, args any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if turn, ok := g.engine.Active(g.conv.ID); ok {
			sc := officetools.Scope{ProjectID: g.f.project.ID, ConversationID: g.conv.ID, RunRef: turn.ID, Agent: g.leadNm}
			if coord, _ := g.engine.WorkflowScope(sc); coord { // its run is set up (the real tools come once it runs)
				return g.engine.WorkflowCall(g.context, sc, name, raw)
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no answer in progress")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (g wfGroup) run(t *testing.T) storage.WorkflowRun {
	t.Helper()
	runs, err := g.f.st.WorkflowRuns().List(g.context, g.f.project.ID, g.conv.ID, 1)
	if err != nil || len(runs) == 0 {
		t.Fatalf("runs = %v, %v", runs, err)
	}
	return runs[0]
}

func TestWorkflowCommitteeRunsInParallelAndCallsBack(t *testing.T) {
	g := newWFGroup(t)
	g.install(t, "hoi-dong", map[string]string{"a": g.dev.ID, "b": g.qa.ID})
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	turn, _, err := g.engine.Send(g.context, g.conv.ID, "/hoi-dong vì sao test chập chờn", nil)
	if err != nil {
		t.Fatal(err)
	}
	brief := map[string]string{"question": "Vì sao test chập chờn?", "context": "Đã thử tăng timeout"}
	if _, err := g.during(t, "workflow_delegate", map[string]any{"role": "a", "brief": brief}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.during(t, "workflow_delegate", map[string]any{"role": "b", "brief": brief}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.during(t, "workflow_delegate", map[string]any{"role": "b", "brief": brief}); err == nil {
		t.Fatal("the same role was given out twice in one turn")
	}
	collect(t, turn)
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	got := g.waitAuthors(t, 4) // lead, Dev and QA (in either order), lead called back once
	if len(got) != 4 || got[0] != g.leadNm || got[3] != g.leadNm {
		t.Fatalf("authors = %v", got)
	}
	args1, in1 := call(t, g.dir, 1)
	if !strings.Contains(args1, "Quy trình đang chạy: Hội đồng") || !strings.Contains(in1, "vì sao test chập chờn") {
		t.Fatalf("coordinator's turn:\n%s\n%s", args1, in1)
	}
	var devIn, back string
	for i := 2; i <= 4; i++ {
		a, in := call(t, g.dir, i)
		switch {
		case strings.Contains(a, "Bạn là Dev"):
			devIn = in
		case strings.Contains(in, "[office · quy trình Hội đồng]"):
			back = in
		}
	}
	if !strings.Contains(devIn, "Vì sao test chập chờn?") || !strings.Contains(devIn, "Chỉ phân tích") {
		t.Fatalf("Dev's brief:\n%s", devIn)
	}
	if !strings.Contains(back, "Thành viên A") || !strings.Contains(back, "Thành viên B") {
		t.Fatalf("call-back:\n%s", back)
	}
	r := g.run(t)
	if r.Status != storage.RunRunning || r.Turns != 2 || r.Roles[0].Status != "done" || r.Roles[1].Status != "done" || r.Roles[0].SessionID == "" {
		t.Fatalf("run = %+v", r)
	}
	// a follow-up resumes the role's own session
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	turn, _, err = g.engine.Send(g.context, g.conv.ID, "hỏi lại A", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.during(t, "workflow_send", map[string]any{"role": "a", "message": "B nói do race, bạn thấy sao?"}); err != nil {
		t.Fatal(err)
	}
	collect(t, turn)
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	g.waitAuthors(t, 7)
	var resumed bool
	for i := 5; i <= 7; i++ {
		if a, in := call(t, g.dir, i); strings.Contains(a, "Bạn là Dev") && strings.Contains(a, "--resume sess-dev") && strings.Contains(in, "race") {
			resumed = true
		}
	}
	if !resumed {
		t.Fatal("the follow-up did not resume Dev's session")
	}
	// done
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	turn, _, err = g.engine.Send(g.context, g.conv.ID, "kết thúc đi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.during(t, "workflow_done", map[string]any{"summary": "Do race."}); err != nil {
		t.Fatal(err)
	}
	collect(t, turn)
	if r := g.run(t); r.Status != storage.RunDone || r.Result != "Do race." || r.FinishedAt == nil {
		t.Fatalf("run after done = %+v", r)
	}
	if g.engine.RunningWorkflow(g.conv.ID) != "" {
		t.Fatal("still running")
	}
}

func TestWorkflowEnforcesItsRules(t *testing.T) {
	g := newWFGroup(t)
	g.install(t, "lam-tinh-nang", map[string]string{"ke-hoach": g.dev.ID, "phan-bien": g.qa.ID, "thuc-thi": g.dev.ID, "review": g.qa.ID})
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("2"), 0o644)
	turn, _, err := g.engine.Send(g.context, g.conv.ID, "/lam-tinh-nang đăng nhập bằng Google", nil)
	if err != nil {
		t.Fatal(err)
	}
	full := map[string]string{"outcome": "Đăng nhập Google", "constraints": "giữ API cũ", "done_when": "test xanh"}
	if _, err := g.during(t, "workflow_delegate", map[string]any{"role": "nope", "brief": full}); err == nil {
		t.Fatal("an unknown role was accepted")
	}
	if _, err := g.during(t, "workflow_delegate", map[string]any{"role": "ke-hoach", "brief": map[string]string{"outcome": "x"}}); err == nil || !strings.Contains(err.Error(), "constraints") {
		t.Fatalf("a brief missing parts: %v", err)
	}
	if _, err := g.during(t, "workflow_send", map[string]any{"role": "ke-hoach", "message": "x"}); err == nil {
		t.Fatal("a follow-up before the role was given out")
	}
	if _, err := g.during(t, "workflow_delegate", map[string]any{"role": "ke-hoach", "brief": full}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.during(t, "workflow_delegate", map[string]any{"role": "phan-bien", "brief": full}); err == nil || !strings.Contains(err.Error(), "parallel") {
		t.Fatalf("two roles not allowed at once: %v", err)
	}
	if _, err := g.during(t, "workflow_done", map[string]any{"summary": "x"}); err == nil || !strings.Contains(err.Error(), "cổng") {
		t.Fatalf("done before the required gates: %v", err)
	}
	if _, err := g.during(t, "workflow_vote", map[string]any{"question": "ok?"}); err == nil {
		t.Fatal("a vote in a workflow without one")
	}
	// the tools are the coordinator's only, and delegate is off in a run
	sc := officetools.Scope{ProjectID: g.f.project.ID, ConversationID: g.conv.ID, RunRef: turn.ID, Agent: g.leadNm}
	if coord, inRun := g.engine.WorkflowScope(sc); !coord || !inRun {
		t.Fatalf("scope = %v %v", coord, inRun)
	}
	if _, err := g.engine.Delegate(g.context, sc, "Dev", "x"); err == nil {
		t.Fatal("delegate inside a workflow")
	}
	collect(t, turn)
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	g.waitAuthors(t, 3)
	// another /workflow while it runs is refused
	if _, _, err := g.engine.Send(g.context, g.conv.ID, "/lam-tinh-nang lần nữa", nil); err != chat.ErrWorkflowRunning {
		t.Fatalf("second run: %v", err)
	}
	// Stop ends it
	g.engine.StopAll(g.conv.ID)
	if r := g.run(t); r.Status != storage.RunStopped {
		t.Fatalf("after stop = %s", r.Status)
	}
}

func TestWorkflowVoteTallies(t *testing.T) {
	g := newWFGroup(t)
	// the 3-party council: the coordinator is the lead, its three roles Dev, QA and a third agent
	aud, _ := g.f.st.Agents().Create(g.context, storage.Agent{ProjectID: g.f.project.ID, Key: "aud", Name: "Aud", ModelTier: "fast"})
	g.install(t, "hoi-dong-3-ben", map[string]string{"lap-ke-hoach": g.dev.ID, "thuc-thi": g.qa.ID, "giam-sat": aud.ID})
	os.WriteFile(filepath.Join(g.dir, "reply-dev"), []byte("PHIẾU: ĐỒNG Ý"), 0o644)
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	turn, _, err := g.engine.Send(g.context, g.conv.ID, "/hoi-dong-3-ben làm X", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.during(t, "workflow_vote", map[string]any{"question": "Thông qua kế hoạch X?"}); err != nil {
		t.Fatal(err)
	}
	collect(t, turn)
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	g.waitAuthors(t, 5)
	// QA and Aud answer "lead: ok" (no ballot): 1 yes of 3, the auditor's unclear vote vetoes
	var back string
	for i := 2; i <= 5; i++ {
		if _, in := call(t, g.dir, i); strings.Contains(in, "Kết quả biểu quyết") {
			back = in
		}
	}
	if !strings.Contains(back, "1/3 đồng ý") || !strings.Contains(back, "KHÔNG THÔNG QUA") || !strings.Contains(back, "phủ quyết") {
		t.Fatalf("call-back:\n%s", back)
	}
}
