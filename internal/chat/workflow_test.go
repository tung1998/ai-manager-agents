package chat_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// own is the chat the workflow called from g.conv runs in.
func (g wfGroup) own(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if runs, _ := g.f.st.WorkflowRuns().List(g.context, g.f.project.ID, g.conv.ID, 1); len(runs) > 0 && runs[0].ConversationID != g.conv.ID {
			return runs[0].ConversationID
		}
		if time.Now().After(deadline) {
			t.Fatal("no run")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitIn waits for n answers in a chat with nothing running there.
func (g wfGroup) waitIn(t *testing.T, conv string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		msgs, _ := g.f.st.Chat().ListMessages(g.context, conv)
		var out []string
		for _, m := range msgs {
			switch m.Role {
			case "assistant":
				out = append(out, m.Author)
			case "error":
				out = append(out, "!")
			}
		}
		_, busy := g.engine.Active(conv)
		if len(out) >= n && !busy && len(g.engine.Running(conv)) == 0 {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %v", out)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// during calls a workflow tool from inside the coordinator's answer in
// progress, in the run's own chat.
func (g wfGroup) during(t *testing.T, name string, args any) (string, error) {
	t.Helper()
	return g.duringIn(t, g.own(t), name, args)
}

// duringIn is during in a given run's chat (a sub-workflow's).
func (g wfGroup) duringIn(t *testing.T, own, name string, args any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if turn, ok := g.engine.Active(own); ok {
			sc := officetools.Scope{ProjectID: g.f.project.ID, ConversationID: own, RunRef: turn.ID, Agent: g.leadNm}
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
	g.install(t, "council", map[string]string{"a": g.dev.ID, "b": g.qa.ID})
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "/council vì sao test chập chờn", nil)
	if err != nil {
		t.Fatal(err)
	}
	own := g.own(t)
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
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	got := g.waitIn(t, own, 4) // lead, Dev and QA (in either order), lead called back once
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
	// a follow-up resumes the role's own session (a coordinator's turn of
	// the run's chat, as office calls it back)
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	turn, _, err := g.engine.Send(g.context, own, "hỏi lại A", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.during(t, "workflow_send", map[string]any{"role": "a", "message": "B nói do race, bạn thấy sao?"}); err != nil {
		t.Fatal(err)
	}
	collect(t, turn)
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	g.waitIn(t, own, 7)
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
	turn, _, err = g.engine.Send(g.context, own, "kết thúc đi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.during(t, "workflow_done", map[string]any{"summary": "Do race.", "outputs": map[string]string{"plan": "sửa race", "agreed": "true"}}); err != nil {
		t.Fatal(err)
	}
	collect(t, turn)
	if r := g.run(t); r.Status != storage.RunDone || r.Result != "Do race." || r.Outputs["agreed"] != "true" || r.FinishedAt == nil {
		t.Fatalf("run after done = %+v", r)
	}
	if g.engine.RunningWorkflow(own) != "" {
		t.Fatal("still running")
	}
	// the chat that called it: only the input and the output
	if evs := collect(t, call0); evs[len(evs)-1].Message == nil || !strings.HasPrefix(evs[len(evs)-1].Message.Content, "Do race.") || !strings.Contains(evs[len(evs)-1].Message.Content, "`plan`: sửa race") {
		t.Fatalf("caller's answer = %+v", evs)
	}
	msgs, _ := g.f.st.Chat().ListMessages(g.context, g.conv.ID)
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" || !strings.HasPrefix(msgs[1].Content, "Do race.") || msgs[1].Author != g.leadNm {
		t.Fatalf("caller's chat = %+v", msgs)
	}
	if c, _ := g.f.st.Chat().GetConversation(g.context, own); c.Purpose != chat.RunPurpose {
		t.Fatalf("own chat purpose = %q", c.Purpose)
	}
}

func TestWorkflowEnforcesItsRules(t *testing.T) {
	g := newWFGroup(t)
	g.install(t, "feature", map[string]string{"ke-hoach": g.dev.ID, "phan-bien": g.qa.ID, "thuc-thi": g.dev.ID, "review": g.qa.ID})
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("2"), 0o644)
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "/feature đăng nhập bằng Google", nil)
	if err != nil {
		t.Fatal(err)
	}
	own := g.own(t)
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
	turn, _ := g.engine.Active(own)
	sc := officetools.Scope{ProjectID: g.f.project.ID, ConversationID: own, RunRef: turn.ID, Agent: g.leadNm}
	if coord, inRun := g.engine.WorkflowScope(sc); !coord || !inRun {
		t.Fatalf("scope = %v %v", coord, inRun)
	}
	if _, err := g.engine.Delegate(g.context, sc, "Dev", "x"); err == nil {
		t.Fatal("delegate inside a workflow")
	}
	collect(t, turn)
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	g.waitIn(t, own, 3)
	// another /workflow while it runs is refused
	if _, _, err := g.engine.Send(g.context, g.conv.ID, "/feature lần nữa", nil); err != chat.ErrWorkflowRunning {
		t.Fatalf("second run: %v", err)
	}
	// Dừng in the chat that called it ends it
	g.engine.StopAll(g.conv.ID)
	if evs := collect(t, call0); evs[len(evs)-1].Type != "error" {
		t.Fatalf("caller's answer after stop = %+v", evs)
	}
	if r := g.run(t); r.Status != storage.RunStopped {
		t.Fatalf("after stop = %s", r.Status)
	}
}

func TestWorkflowVoteTallies(t *testing.T) {
	g := newWFGroup(t)
	// the 3-party council: the coordinator is the lead, its three roles Dev, QA and a third agent
	aud, _ := g.f.st.Agents().Create(g.context, storage.Agent{ProjectID: g.f.project.ID, Key: "aud", Name: "Aud", ModelTier: "fast"})
	g.install(t, "council-3", map[string]string{"lap-ke-hoach": g.dev.ID, "thuc-thi": g.qa.ID, "giam-sat": aud.ID})
	os.WriteFile(filepath.Join(g.dir, "reply-dev"), []byte("PHIẾU: ĐỒNG Ý"), 0o644)
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	if _, _, err := g.engine.Send(g.context, g.conv.ID, "/council-3 làm X", nil); err != nil {
		t.Fatal(err)
	}
	own := g.own(t)
	if _, err := g.during(t, "workflow_vote", map[string]any{"question": "Thông qua kế hoạch X?"}); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	g.waitIn(t, own, 5)
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

// a role filled by another workflow: it runs in a chat of its own, its
// output is the role's answer in the run that called it (ADR-102)
func TestWorkflowCallsASubWorkflow(t *testing.T) {
	g := newWFGroup(t)
	g.install(t, "advisor", map[string]string{"co-van": g.dev.ID})
	parent := "---\nkey: cha\nname: Cha\ndescription: gọi con\nroles:\n  - key: con\n    name: Con\n    workflow: advisor\nlimits:\n  depth: 1\n---\nGiao vai con rồi tổng kết.\n"
	if _, err := g.svc.Create(g.context, g.f.project.ID, parent, map[string]string{"con": g.lead}); err != nil { // the coordinator runs the sub-workflow too
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("3"), 0o644)
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "/cha nên làm gì", nil)
	if err != nil {
		t.Fatal(err)
	}
	own := g.own(t)
	if _, err := g.duringIn(t, own, "workflow_delegate", map[string]any{"role": "con", "brief": map[string]string{"question": "Q?"}}); err != nil {
		t.Fatal(err)
	}
	// the sub-workflow's own chat, called from the parent's
	var child storage.WorkflowRun
	deadline := time.Now().Add(10 * time.Second)
	for child.ID == "" {
		runs, _ := g.f.st.WorkflowRuns().List(g.context, g.f.project.ID, own, 5)
		for _, r := range runs {
			if r.CallerConversationID == own {
				child = r
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no sub-workflow run")
		}
		time.Sleep(20 * time.Millisecond)
	}
	parentRun := g.run(t)
	if child.ParentRunID != parentRun.ID || child.Depth != 1 || child.WorkflowKey != "advisor" || !strings.Contains(child.Input, "Q?") {
		t.Fatalf("child = %+v", child)
	}
	// limits.depth 1: the child cannot go deeper (advisor has no sub-workflow anyway)
	if _, err := g.duringIn(t, child.ConversationID, "workflow_done", map[string]any{"summary": "Ý kiến con", "outputs": map[string]string{"recommendation": "làm A"}}); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	deadline = time.Now().Add(10 * time.Second)
	for {
		r := g.run(t)
		if len(r.Roles) == 1 && r.Roles[0].Status == "done" && strings.HasPrefix(r.Roles[0].Result, "Ý kiến con") && r.Roles[0].RunID == child.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("parent run = %+v", r)
		}
		time.Sleep(50 * time.Millisecond)
	}
	msgs, _ := g.f.st.Chat().ListMessages(g.context, own)
	if !slices.ContainsFunc(msgs, func(m storage.Message) bool {
		return strings.Contains(m.Content, "Quy trình con /advisor") && strings.Contains(m.Content, "Ý kiến con")
	}) {
		t.Fatalf("parent's chat = %+v", msgs)
	}
	g.engine.StopAll(g.conv.ID)
	collect(t, call0)
}

// a workflow calling itself: allowed, as deep as limits.depth
func TestWorkflowRecursionStopsAtItsDepth(t *testing.T) {
	g := newWFGroup(t)
	self := "---\nkey: de-quy\nname: Đệ quy\ndescription: chia nhỏ\nroles:\n  - key: con\n    name: Phần nhỏ hơn\n    workflow: de-quy\nlimits:\n  depth: 1\n---\nChia việc, giao phần nhỏ hơn cho chính quy trình này.\n"
	if _, err := g.svc.Create(g.context, g.f.project.ID, self, map[string]string{"con": g.lead}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("3"), 0o644)
	defer os.Remove(filepath.Join(g.dir, "sleep-lead"))
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "/de-quy việc lớn", nil)
	if err != nil {
		t.Fatal(err)
	}
	own := g.own(t)
	if _, err := g.duringIn(t, own, "workflow_delegate", map[string]any{"role": "con", "brief": map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	var child string
	deadline := time.Now().Add(10 * time.Second)
	for child == "" {
		runs, _ := g.f.st.WorkflowRuns().List(g.context, g.f.project.ID, own, 5)
		for _, r := range runs {
			if r.CallerConversationID == own {
				child = r.ConversationID
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no sub-run")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// one level down it may not go further
	if _, err := g.duringIn(t, child, "workflow_delegate", map[string]any{"role": "con", "brief": map[string]string{}}); err == nil || !strings.Contains(err.Error(), "limits.depth") {
		t.Fatalf("deeper than limits.depth: %v", err)
	}
	// stopping the top run stops the one it called
	g.engine.StopAll(g.conv.ID)
	collect(t, call0)
	deadline = time.Now().Add(5 * time.Second)
	for g.engine.RunningWorkflow(child) != "" {
		if time.Now().After(deadline) {
			t.Fatal("the sub-run still runs")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// declared inputs and outputs, and a workflow only other workflows call (ADR-103)
func TestSubWorkflowInputsOutputs(t *testing.T) {
	g := newWFGroup(t)
	child := "---\nkey: con-ra\nname: Con\ncallable: sub\ninputs:\n  - key: cau-hoi\n    required: true\noutputs:\n  - key: ket-luan\n    required: true\nroles:\n  - key: x\n---\nTrả lời câu hỏi.\n"
	parent := "---\nkey: cha2\nname: Cha\nroles:\n  - key: con\n    workflow: con-ra\n---\nGiao vai con.\n"
	if _, err := g.svc.Create(g.context, g.f.project.ID, child, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := g.svc.Create(g.context, g.f.project.ID, parent, map[string]string{"con": g.lead}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.engine.Send(g.context, g.conv.ID, "/con-ra thử", nil); err == nil || !strings.Contains(err.Error(), "callable") {
		t.Fatalf("a sub-only workflow ran from the chat: %v", err)
	}
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("3"), 0o644)
	defer os.Remove(filepath.Join(g.dir, "sleep-lead"))
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "/cha2 làm đi", nil)
	if err != nil {
		t.Fatal(err)
	}
	own := g.own(t)
	if _, err := g.duringIn(t, own, "workflow_delegate", map[string]any{"role": "con", "brief": map[string]string{}}); err == nil || !strings.Contains(err.Error(), "cau-hoi") {
		t.Fatalf("a missing input: %v", err)
	}
	if _, err := g.duringIn(t, own, "workflow_delegate", map[string]any{"role": "con", "brief": map[string]string{"cau-hoi": "Q?"}}); err != nil {
		t.Fatal(err)
	}
	var sub storage.WorkflowRun
	deadline := time.Now().Add(10 * time.Second)
	for sub.ID == "" {
		runs, _ := g.f.st.WorkflowRuns().List(g.context, g.f.project.ID, own, 5)
		for _, r := range runs {
			if r.CallerConversationID == own {
				sub = r
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no sub-run")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(sub.Input, "Q?") {
		t.Fatalf("sub input = %q", sub.Input)
	}
	if _, err := g.duringIn(t, sub.ConversationID, "workflow_done", map[string]any{"summary": "xong"}); err == nil || !strings.Contains(err.Error(), "ket-luan") {
		t.Fatalf("done without its outputs: %v", err)
	}
	if _, err := g.duringIn(t, sub.ConversationID, "workflow_done", map[string]any{"summary": "xong", "outputs": map[string]string{"ket-luan": "A"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		r := g.run(t)
		if r.Roles[0].Status == "done" && strings.Contains(r.Roles[0].Result, "`ket-luan`: A") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("parent run = %+v", r)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got, _ := g.f.st.WorkflowRuns().Get(g.context, sub.ID); got.Outputs["ket-luan"] != "A" {
		t.Fatalf("outputs = %v", got.Outputs)
	}
	g.engine.StopAll(g.conv.ID)
	collect(t, call0)
}

// workflow_ask: the coordinator gets an analyze role's answer within its own
// turn, and is not called back for it (ADR-104)
func TestWorkflowAskWaitsForTheAnswer(t *testing.T) {
	g := newWFGroup(t)
	g.install(t, "advisor", map[string]string{"co-van": g.dev.ID})
	os.WriteFile(filepath.Join(g.dir, "reply-dev"), []byte("Nên dùng hàng đợi."), 0o644)
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("3"), 0o644)
	defer os.Remove(filepath.Join(g.dir, "sleep-lead"))
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "/advisor có nên dùng hàng đợi", nil)
	if err != nil {
		t.Fatal(err)
	}
	own := g.own(t)
	if _, err := g.duringIn(t, own, "workflow_ask", map[string]any{"role": "nope", "question": "?"}); err == nil {
		t.Fatal("an unknown role was asked")
	}
	got, err := g.duringIn(t, own, "workflow_ask", map[string]any{"role": "co-van", "question": "Hàng đợi hay gọi thẳng?", "wait_seconds": 20})
	if err != nil || !strings.Contains(got, "Nên dùng hàng đợi.") {
		t.Fatalf("ask = %q, %v", got, err)
	}
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	authors := g.waitIn(t, own, 2)
	time.Sleep(300 * time.Millisecond) // no call-back follows
	if authors = g.waitIn(t, own, 2); len(authors) != 2 {
		t.Fatalf("authors = %v", authors)
	}
	if r := g.run(t); r.Roles[0].Status != "done" || r.Turns != 1 {
		t.Fatalf("run = %+v", r)
	}
	g.engine.StopAll(g.conv.ID)
	collect(t, call0)
}

// the run's own chat starts empty: its coordinator gets the end of the chat
// that called it
func TestWorkflowSeesTheCallingChat(t *testing.T) {
	g := newWFGroup(t)
	g.install(t, "handoff", map[string]string{"nguoi-nhan": g.dev.ID})
	g.sendAll(t, "API /orders trả 500 khi giỏ hàng trống")
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	defer os.Remove(filepath.Join(g.dir, "sleep-lead"))
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "/handoff giao Dev sửa lỗi trên", nil)
	if err != nil {
		t.Fatal(err)
	}
	g.own(t)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, in := call(t, g.dir, 2); strings.Contains(in, "cuộc chat đã gọi quy trình") {
			if !strings.Contains(in, "API /orders trả 500") {
				t.Fatalf("coordinator's input lacks the chat:\n%s", in)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no coordinator call")
		}
		time.Sleep(50 * time.Millisecond)
	}
	g.engine.StopAll(g.conv.ID)
	collect(t, call0)
}

// a graph of steps runs in order without a coordinator: code, a condition
// that loops back, an HTTP request, an agent, the end (ADR-108)
func TestWorkflowStepsRunInOrder(t *testing.T) {
	g := newWFGroup(t)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		body, _ := io.ReadAll(r.Body)
		w.Write([]byte(`{"ok":true,"got":` + strconv.Quote(string(body)) + `}`))
	}))
	defer srv.Close()
	os.WriteFile(filepath.Join(g.dir, "reply-dev"), []byte("Tóm tắt: ổn"), 0o644)
	src := `---
key: buoc
name: Theo bước
inputs:
  - { key: ten, required: true }
outputs:
  - { key: ket-qua, required: true }
  - { key: so-lan, type: number, required: true }
roles:
  - { key: viet, name: Người viết, access: analyze }
steps:
  - id: dem
    type: code
    lang: bash
    script: |
      n=$(cat "$HOME/.dem" 2>/dev/null || echo 0); n=$((n+1)); echo $n > "$HOME/.dem"
      echo "{\"n\": $n, \"ten\": \"$OFFICE_INPUT_TEN\"}"
    next: du-chua
  - id: du-chua
    type: condition
    if: "{{steps.dem.json.n}} >= 2"
    then: goi
    else: dem
  - id: goi
    type: http
    method: POST
    url: ` + srv.URL + `
    body: '{"ten": "{{steps.dem.json.ten}}"}'
    next: tom-tat
  - id: tom-tat
    type: agent
    role: viet
    prompt: "Tóm tắt kết quả: {{steps.goi.output}}"
    next: xong
  - id: xong
    type: end
    summary: "{{steps.tom-tat.output}}"
    outputs:
      ket-qua: "{{steps.goi.json.ok}}"
      so-lan: "{{steps.dem.json.n}}"
---
`
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := g.svc.Create(g.context, g.f.project.ID, src, map[string]string{"viet": g.dev.ID}); err != nil {
		t.Fatal(err)
	}
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "#buoc An", nil) // "#key": a workflow (ADR-109)
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, call0)
	last := evs[len(evs)-1]
	if last.Message == nil || !strings.Contains(last.Message.Content, "Tóm tắt: ổn") || !strings.Contains(last.Message.Content, "`so-lan`: 2") || !strings.Contains(last.Message.Content, "`ket-qua`: true") {
		t.Fatalf("caller's answer = %+v", last)
	}
	r := g.run(t)
	if r.Status != storage.RunDone || hits != 1 || r.Outputs["so-lan"] != "2" {
		t.Fatalf("run = %+v, hits %d", r, hits)
	}
	// the run's chat holds each step: the code twice, the request, the agent's answer
	msgs, _ := g.f.st.Chat().ListMessages(g.context, r.ConversationID)
	var code, req, dev int
	for _, m := range msgs {
		switch {
		case strings.Contains(m.Content, "dem (code)"):
			code++
		case strings.Contains(m.Content, "goi (http)") && strings.Contains(m.Content, `\"ten\": \"An\"`):
			req++
		case m.Author == "Dev":
			dev++
		}
	}
	if code != 2 || req != 1 || dev != 1 {
		t.Fatalf("run's chat: code %d, request %d, dev %d\n%+v", code, req, dev, msgs)
	}
}
