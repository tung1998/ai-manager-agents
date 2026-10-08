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
	if !strings.Contains(args1, "Running workflow: Hội đồng") || !strings.Contains(in1, "vì sao test chập chờn") {
		t.Fatalf("coordinator's turn:\n%s\n%s", args1, in1)
	}
	var devIn, back string
	for i := 2; i <= 4; i++ {
		a, in := call(t, g.dir, i)
		switch {
		case strings.Contains(a, "You are Dev"):
			devIn = in
		case strings.Contains(in, "[office · workflow Hội đồng]"):
			back = in
		}
	}
	if !strings.Contains(devIn, "Vì sao test chập chờn?") || !strings.Contains(devIn, "Analyze only") {
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
		if a, in := call(t, g.dir, i); strings.Contains(a, "You are Dev") && strings.Contains(a, "--resume sess-dev") && strings.Contains(in, "race") {
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
	os.WriteFile(filepath.Join(g.dir, "reply-dev"), []byte("VOTE: AGREE"), 0o644)
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
		if _, in := call(t, g.dir, i); strings.Contains(in, "Vote result") {
			back = in
		}
	}
	if !strings.Contains(back, "1/3 agree") || !strings.Contains(back, "NOT PASSED") || !strings.Contains(back, "Vetoed") {
		t.Fatalf("call-back:\n%s", back)
	}
}

const strictVoteSource = `---
key: strict-vote
name: Strict Vote
description: test workflow with a vote and a differ_from
input: X
inputs:
  - { key: x, description: "x", required: true }
outputs:
  - { key: ok, description: "ok", type: boolean, required: true }
roles:
  - key: a
    name: A
    hint: role a
    access: analyze
  - key: b
    name: B
    hint: role b
    access: analyze
    differ_from: [a]
vote:
  roles: [a, b]
  quorum: 1
limits: { rounds: 2, turns: 8, timeout: 1h }
---
1. Put something to a vote with workflow_vote.
2. Call workflow_done.
`

// TestWorkflowVoteChecksDifferFrom: wfVote must enforce differ_from for
// roles picked fresh in the vote itself (no prior binding), not just at
// workflow_delegate/workflow_ask time.
func TestWorkflowVoteChecksDifferFrom(t *testing.T) {
	g := newWFGroup(t)
	w, err := g.svc.Create(g.context, g.f.project.ID, strings.Replace(strictVoteSource, "key: strict-vote", "key: strict-vote\nstrict: true", 1), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.engine.Send(g.context, g.conv.ID, "/"+w.Key+" làm X", nil); err != nil {
		t.Fatal(err)
	}
	// a and b are both unbound; picking the same-family Dev and QA for the
	// vote at once must be refused in strict mode, even though neither role
	// has an AgentID saved yet.
	if _, err := g.during(t, "workflow_vote", map[string]any{"question": "ok?", "agents": map[string]string{"a": g.dev.Name, "b": g.qa.Name}}); err == nil || !strings.Contains(err.Error(), "khác hãng") {
		t.Fatalf("strict vote with same-family roles should be refused, got: %v", err)
	}
}

// TestWorkflowVoteWarnsOnSharedFamily: a non-strict workflow lets the vote
// through but records a warning note told back to the coordinator.
func TestWorkflowVoteWarnsOnSharedFamily(t *testing.T) {
	g := newWFGroup(t)
	w, err := g.svc.Create(g.context, g.f.project.ID, strictVoteSource, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	if _, _, err := g.engine.Send(g.context, g.conv.ID, "/"+w.Key+" làm X", nil); err != nil {
		t.Fatal(err)
	}
	own := g.own(t)
	if _, err := g.during(t, "workflow_vote", map[string]any{"question": "ok?", "agents": map[string]string{"a": g.dev.Name, "b": g.qa.Name}}); err != nil {
		t.Fatalf("non-strict vote with same-family roles should still be sent: %v", err)
	}
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	g.waitIn(t, own, 3)
	var back string
	for i := 2; i <= 4; i++ {
		if _, in := call(t, g.dir, i); strings.Contains(in, "Vote result") {
			back = in
		}
	}
	if !strings.Contains(back, "must use a different model vendor") {
		t.Fatalf("call-back should carry the differ_from warning:\n%s", back)
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
		if _, in := call(t, g.dir, 2); strings.Contains(in, "the chat that called this workflow") {
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

// one output goes to several steps at once; a join waits for both
// branches and runs once; a switch takes the case its value matches; a
// failure is an output for the steps after it (on_error: continue)
func TestWorkflowStepsBranchAndJoin(t *testing.T) {
	g := newWFGroup(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := `---
key: nhanh
name: Nhánh
inputs:
  - { key: a, required: true }
  - { key: b }
outputs:
  - { key: gom, required: true }
  - { key: loai, required: true }
  - { key: loi }
roles:
  - { key: viet, name: Người viết, access: analyze }
start: [mot, hai]
steps:
  - id: mot
    type: code
    lang: bash
    script: sleep 1; echo "1-$OFFICE_INPUT_A"
    next: gom
  - id: hai
    type: code
    lang: bash
    script: sleep 1; echo "2-$OFFICE_INPUT_B"
    next: [gom, hong]
  - id: hong
    type: code
    lang: bash
    script: echo boom; exit 3
    on_error: continue
    next: gom
  - id: gom
    type: code
    lang: bash
    script: echo "{\"kind\":\"bug\"}"; echo x >> "$HOME/.gom"
    next: chon
  - id: chon
    type: switch
    value: "{{steps.gom.json.kind}}"
    cases:
      - { when: feature, next: sai }
      - { when: BUG, next: xong }
    else: sai
  - id: sai
    type: end
    outputs: { gom: sai, loai: sai }
  - id: xong
    type: end
    summary: "{{steps.mot.output}} {{steps.hai.output}}"
    outputs:
      gom: "{{steps.mot.output}}+{{steps.hai.output}}"
      loai: "{{steps.chon.status}}"
      loi: "{{steps.hong.status}}:{{steps.hong.output}}"
---
`
	if _, err := g.svc.Create(g.context, g.f.project.ID, src, map[string]string{"viet": g.dev.ID}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "#nhanh {\"a\": \"x\", \"b\": \"y\"}", nil)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, call0)
	r := g.run(t)
	if r.Status != storage.RunDone || r.Outputs["gom"] != "1-x+2-y" || r.Outputs["loai"] != "bug" || r.Outputs["loi"] != "3:boom" {
		t.Fatalf("run = %+v", r)
	}
	if took := time.Since(start); took > 1900*time.Millisecond {
		t.Fatalf("the two branches did not run at once: %v", took)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".gom")); string(b) != "x\n" {
		t.Fatalf("the join ran %q", b)
	}
}

// a coordinate step (ADR-111): an agent decides inside the step, in a run
// of its own; its workflow_done is the step's output for the steps after it
func TestWorkflowCoordinateStep(t *testing.T) {
	g := newWFGroup(t)
	src := `---
key: dp
name: Điều phối trong bước
inputs:
  - { key: spec, required: true }
outputs:
  - { key: ket-luan, required: true }
roles:
  - { key: a, name: A, access: analyze }
  - { key: b, name: B, access: analyze }
steps:
  - id: review
    type: coordinate
    roles: [a]
    prompt: "Giao a review {{input.spec}}; kết luận dong-y | can-sua."
    next: chon
  - id: chon
    type: switch
    value: "{{steps.review.json.ket-luan}}"
    cases:
      - { when: dong-y, next: xong }
    else: sai
  - { id: sai, type: end, outputs: { ket-luan: sai } }
  - id: xong
    type: end
    summary: "{{steps.review.output}}"
    outputs: { ket-luan: "{{steps.chon.status}}" }
---
`
	if _, err := g.svc.Create(g.context, g.f.project.ID, src, map[string]string{"a": g.dev.ID, "b": g.qa.ID}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("3"), 0o644)
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "#dp đặc tả X", nil)
	if err != nil {
		t.Fatal(err)
	}
	own := g.own(t)
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
			t.Fatal("no coordinate run")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(child.Roles) != 1 || child.Roles[0].Role != "a" || child.Depth != 1 || !strings.Contains(child.WorkflowName, "review") {
		t.Fatalf("child = %+v", child)
	}
	if _, err := g.duringIn(t, child.ConversationID, "workflow_done", map[string]any{"summary": "A thấy ổn", "outputs": map[string]string{"ket-luan": "dong-y"}}); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	collect(t, call0)
	r := g.run(t)
	if r.Status != storage.RunDone || r.Outputs["ket-luan"] != "dong-y" || !strings.Contains(r.Result, "A thấy ổn") {
		t.Fatalf("run = %+v", r)
	}
}

// a graph of steps has no coordinator deciding: a role bound to the agent
// that runs it (the chat's) is filled by that agent, not dropped
func TestWorkflowStepsRoleOfTheRunsAgent(t *testing.T) {
	g := newWFGroup(t)
	src := `---
key: chinh-minh
name: Chính mình
roles:
  - { key: tong-hop, name: Tổng hợp, access: analyze }
steps:
  - { id: tong_hop, type: agent, role: tong-hop, prompt: "Tổng hợp", next: xong }
  - { id: xong, type: end }
---
`
	if _, err := g.svc.Create(g.context, g.f.project.ID, src, map[string]string{"tong-hop": g.lead}); err != nil {
		t.Fatal(err)
	}
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "#chinh-minh x", nil)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, call0)
	r := g.run(t)
	if r.Status != storage.RunDone || r.Roles[0].AgentID != g.lead {
		t.Fatalf("run = %+v", r)
	}
}

// "#key" in a Burn piece's review chat (ADR-113, ADR-114) runs the workflow, as in
// a team chat, and its answer is the workflow's output: not a plain message.
func TestWorkflowRunsFromABurnChat(t *testing.T) {
	g := newWFGroup(t)
	src := `---
key: review-burn
name: Review Burn
inputs:
  - { key: yeu-cau, required: true }
steps:
  - id: xem
    type: code
    lang: bash
    script: 'echo "KẾT LUẬN: ĐỒNG Ý"'
    next: xong
  - id: xong
    type: end
    summary: "{{steps.xem.output}}"
---
`
	if _, err := g.svc.Create(g.context, g.f.project.ID, src, nil); err != nil {
		t.Fatal(err)
	}
	conv, err := g.engine.StartConversationPurpose(g.context, g.f.project.ID, "", chat.BurnReviewPurpose)
	if err != nil {
		t.Fatal(err)
	}
	turn, _, err := g.engine.Send(g.context, conv.ID, "#review-burn [Burn · Review kết quả] việc x", nil)
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, turn)
	last := evs[len(evs)-1]
	if last.Message == nil || !strings.Contains(last.Message.Content, "KẾT LUẬN: ĐỒNG Ý") {
		t.Fatalf("answer = %+v", last)
	}
	runs, _ := g.f.st.WorkflowRuns().List(g.context, g.f.project.ID, conv.ID, 1)
	if len(runs) != 1 || runs[0].Status != storage.RunDone {
		t.Fatalf("runs = %+v", runs)
	}
}

// a role that finds its brief wrong pushes back (ADR-115): the coordinator
// is told, and the run ends only once it is answered or overruled
func TestWorkflowRolePushesBack(t *testing.T) {
	g := newWFGroup(t)
	src := `---
key: pb
name: Phản biện
roles:
  - { key: a, name: A, access: analyze }
limits: { turns: 4 }
---
Giao a rồi kết thúc.
`
	if _, err := g.svc.Create(g.context, g.f.project.ID, src, map[string]string{"a": g.dev.ID}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(g.dir, "raw-dev"), []byte(`PHẢN BIỆN: bản giao sửa nhầm file\nbằng chứng: a.go:1`), 0o644)
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("2"), 0o644)
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "#pb việc X", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.during(t, "workflow_delegate", map[string]any{"role": "a", "brief": map[string]string{"outcome": "sửa b.go"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for g.run(t).Roles[0].Objection == "" {
		if time.Now().After(deadline) {
			t.Fatalf("no objection: %+v", g.run(t).Roles)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := g.during(t, "workflow_done", map[string]any{"summary": "xong"}); err == nil || !strings.Contains(err.Error(), "phản biện") {
		t.Fatalf("done with an objection unanswered: %v", err)
	}
	if _, err := g.during(t, "workflow_done", map[string]any{"summary": "xong", "overrule": "người dùng chỉ định b.go"}); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	collect(t, call0)
	r := g.run(t)
	if r.Status != storage.RunDone || !strings.Contains(r.Result, "sửa nhầm file") || !strings.Contains(r.Result, "người dùng chỉ định b.go") {
		t.Fatalf("run = %+v", r)
	}
	// the coordinator was told on its call-back
	deadline = time.Now().Add(5 * time.Second)
	for !slices.ContainsFunc(argsIn(t, g.dir), func(s string) bool { return strings.Contains(s, "OBJECTS to the brief") }) {
		if time.Now().After(deadline) {
			t.Fatal("coordinator not told of the objection")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// a supervisor checks the run while roles work (ADR-115); drift it sees is
// told to the coordinator when it is called back
func TestWorkflowSupervisorTellsDrift(t *testing.T) {
	defer chat.SetWatchEvery(150 * time.Millisecond)()
	g := newWFGroup(t)
	src := `---
key: sv
name: Có giám sát
roles:
  - { key: a, name: A, access: analyze }
  - { key: gs, name: Giám sát, access: analyze }
supervise: { role: gs, every: 5m }
---
Giao a rồi kết thúc.
`
	if _, err := g.svc.Create(g.context, g.f.project.ID, src, map[string]string{"a": g.dev.ID, "gs": g.qa.ID}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(g.dir, "raw-qa"), []byte(`GIÁM SÁT: LỆCH: đang làm ngoài phạm vi`), 0o644)
	os.WriteFile(filepath.Join(g.dir, "sleep-dev"), []byte("2"), 0o644)
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	call0, _, err := g.engine.Send(g.context, g.conv.ID, "#sv việc X", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.during(t, "workflow_delegate", map[string]any{"role": "gs", "brief": map[string]string{"outcome": "x"}}); err == nil {
		t.Fatal("work given to the supervisor")
	}
	if _, err := g.during(t, "workflow_delegate", map[string]any{"role": "a", "brief": map[string]string{"outcome": "x"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for !slices.ContainsFunc(argsIn(t, g.dir), func(s string) bool { return strings.Contains(s, "The supervisor (QA) sees drift") }) {
		if time.Now().After(deadline) {
			t.Fatal("the coordinator was not told of the drift")
		}
		time.Sleep(50 * time.Millisecond)
	}
	r := g.run(t)
	if !slices.ContainsFunc(r.Log, func(l storage.RunLog) bool { return strings.Contains(l.Text, "ngoài phạm vi") }) {
		t.Fatalf("log = %+v", r.Log)
	}
	g.engine.StopAll(g.conv.ID)
	collect(t, call0)
}

// argsIn is what every fake CLI call was given (arguments and stdin).
func argsIn(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	files, _ := filepath.Glob(filepath.Join(dir, "call*"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		out = append(out, string(b))
	}
	return out
}
