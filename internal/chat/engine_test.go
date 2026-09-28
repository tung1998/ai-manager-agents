package chat_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/officetools"
	"bitbucket.org/senprints/agent-office/internal/orgmodel"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

type fixture struct {
	st      storage.Store
	engine  *chat.Engine
	provs   *provider.Service
	project storage.Repo
	dir     string
}

func setup(t *testing.T, providerIn func(provs *provider.Service) storage.Provider) fixture {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	provs := provider.NewService(st, box, llm.Options{})
	u := usage.New(st, time.UTC)
	provs.SetUsage(u)
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	providerIn(provs)

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\nworld\n"), 0o644)
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "demo", Path: dir})
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	return fixture{st: st, engine: chat.NewEngine(st, provs, u), provs: provs, project: project, dir: dir}
}

func collect(t *testing.T, turn *chat.Turn) []chat.Event {
	t.Helper()
	deadline := time.After(15 * time.Second)
	seq := 0
	var all []chat.Event
	for {
		evs, done, wake := turn.Since(seq)
		all = append(all, evs...)
		seq += len(evs)
		if done {
			return all
		}
		select {
		case <-wake:
		case <-deadline:
			t.Fatalf("turn did not finish; events: %+v", all)
		}
	}
}

func TestAnthropicToolLoopAndPatch(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Messages []json.RawMessage `json:"messages"`
			System   string            `json:"system"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body.System, "không ghi file trực tiếp") {
			t.Errorf("system prompt missing patch rule: %q", body.System)
		}
		if calls == 1 {
			w.Write([]byte(`{"model":"claude-sonnet-5","stop_reason":"tool_use","content":[{"type":"text","text":"Để tôi đọc file."},{"type":"tool_use","id":"tu1","name":"read_file","input":{"path":"hello.txt"}}],"usage":{"input_tokens":100,"output_tokens":20}}`))
			return
		}
		// the tool result must have been sent back
		last := string(body.Messages[len(body.Messages)-1])
		if !strings.Contains(last, "1\\thello") {
			t.Errorf("tool result not returned: %s", last)
		}
		answer := "File có 2 dòng. Sửa như sau:\n```diff\n--- a/hello.txt\n+++ b/hello.txt\n@@ -1,2 +1,2 @@\n-hello\n+xin chào\n world\n```"
		out, _ := json.Marshal(map[string]any{"model": "claude-sonnet-5", "stop_reason": "end_turn",
			"content": []map[string]any{{"type": "text", "text": answer}}, "usage": map[string]int{"input_tokens": 150, "output_tokens": 60}})
		w.Write(out)
	}))
	defer srv.Close()
	f := setup(t, func(provs *provider.Service) storage.Provider {
		key := "sk-ant-test-key-0000"
		p, err := provs.Create(context.Background(), provider.Input{Name: "C", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: &key})
		if err != nil {
			t.Fatal(err)
		}
		return p
	})
	ctx := actor.With(context.Background(), "human:a@b.c")
	conv, err := f.engine.StartConversation(ctx, f.project.ID, "")
	if err != nil || conv.AgentName != "Trợ lý" {
		t.Fatalf("conversation = %+v, %v", conv, err)
	}
	turn, _, err := f.engine.Send(ctx, conv.ID, "Sửa lời chào trong hello.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.engine.Send(ctx, conv.ID, "again", nil); err != chat.ErrBusy {
		t.Fatalf("concurrent send err = %v", err)
	}
	events := collect(t, turn)
	types := []string{}
	for _, e := range events {
		types = append(types, e.Type)
	}
	joined := strings.Join(types, ",")
	if !strings.Contains(joined, "tool") || !strings.Contains(joined, "patch") || !strings.HasSuffix(joined, "done") {
		t.Fatalf("event types = %s", joined)
	}
	done := events[len(events)-1].Message
	if len(done.Patches) != 1 || done.Patches[0].Status != "pending" || done.Patches[0].Files[0] != "hello.txt" || len(done.Tools) != 1 {
		t.Fatalf("done message = %+v", done)
	}
	if done.CostUSD == nil {
		t.Fatal("chat cost not recorded")
	}
	runs, _ := f.st.Runs().List(ctx, storage.RunFilter{Limit: 5})
	if len(runs) != 1 || runs[0].Kind != "chat" || runs[0].InputTokens != 250 {
		t.Fatalf("runs = %+v", runs)
	}

	// history keeps messages and patches
	hist, _ := f.engine.History(ctx, conv.ID)
	if len(hist) != 2 || hist[0].Role != "user" || len(hist[1].Patches) != 1 {
		t.Fatalf("history = %+v", hist)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	p, err := f.engine.DecidePatch(ctx, done.Patches[0].ID, true)
	if err != nil || p.Status != "applied" || p.DecidedBy != "human:a@b.c" {
		t.Fatalf("approve = %+v, %v", p, err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.dir, "hello.txt")); string(b) != "xin chào\nworld\n" {
		t.Fatalf("file after approve = %q", b)
	}
	if _, err := f.engine.DecidePatch(ctx, done.Patches[0].ID, false); err != chat.ErrDecided {
		t.Fatalf("second decision err = %v", err)
	}
}

func TestClaudeCLIRunnerStreams(t *testing.T) {
	tmp := t.TempDir()
	bin, argsLog := filepath.Join(tmp, "claude"), filepath.Join(tmp, "args")
	os.WriteFile(bin, []byte(`#!/bin/sh
echo "$*" >> `+argsLog+`
case "$*" in *"--tools Read,Glob,Grep"*) ;; *) echo "missing read-only tools: $*" >&2; exit 2;; esac
case "$*" in *"--permission-mode dontAsk"*) ;; *) echo "may ask or bypass: $*" >&2; exit 2;; esac
cat >/dev/null
echo '{"type":"system","subtype":"init","session_id":"sess-1"}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Xin "}}}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"hello.txt"}}]}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"chào"}}}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"Xin chào","session_id":"sess-1","total_cost_usd":0.02,"usage":{"input_tokens":5,"cache_read_input_tokens":100,"output_tokens":7}}'
`), 0o755)
	f := setup(t, func(provs *provider.Service) storage.Provider {
		p, err := provs.Create(context.Background(), provider.Input{Name: "CC", Kind: storage.ProviderClaudeCLI, BaseURL: bin})
		if err != nil {
			t.Fatal(err)
		}
		return p
	})
	ctx := context.Background()
	conv, _ := f.engine.StartConversation(ctx, f.project.ID, "")
	turn, _, err := f.engine.Send(ctx, conv.ID, "chào", nil)
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, turn)
	var text strings.Builder
	for _, e := range events {
		if e.Type == "text" {
			text.WriteString(e.Text)
		}
		if e.Type == "error" {
			t.Fatalf("error event: %s", e.Text)
		}
	}
	done := events[len(events)-1].Message
	if text.String() != "Xin chào" || done.Content != "Xin chào" || len(done.Tools) != 1 || done.Tools[0].Summary != "Đọc hello.txt" {
		t.Fatalf("streamed=%q done=%+v", text.String(), done)
	}
	members, _ := f.st.Chat().Members(ctx, conv.ID) // the session is the agent's (ADR-044)
	if len(members) != 1 || members[0].SessionID != "sess-1" || members[0].Runtime != "claude_cli" || members[0].LastMessageID == "" {
		t.Fatalf("session not saved: %+v", members)
	}
	runs, _ := f.st.Runs().List(ctx, storage.RunFilter{Limit: 5})
	if len(runs) != 1 || runs[0].CostSource != "provider" || runs[0].InputTokens != 105 {
		t.Fatalf("runs = %+v", runs)
	}
	// default: the user's own CLI setup, with skills
	b, _ := os.ReadFile(argsLog)
	if a := string(b); !strings.Contains(a, "--setting-sources user,project,local") || strings.Contains(a, "--strict-mcp-config") ||
		!strings.Contains(a, "--tools Read,Glob,Grep,Skill") {
		t.Fatalf("default args = %s", a)
	}
}

// After an answer, the conversation knows how full its context is and the
// provider's latest usage windows are kept (shown in chat and the sidebar).
func TestClaudeCLIKeepsContextAndLimits(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "claude")
	os.WriteFile(bin, []byte(`#!/bin/sh
cat >/dev/null
echo '{"type":"system","subtype":"init","session_id":"sess-1"}'
echo '{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.2,"resetsAt":1790593200}}}}'
echo '{"type":"assistant","message":{"model":"claude-sonnet-5","usage":{"input_tokens":4,"cache_read_input_tokens":1000,"cache_creation_input_tokens":0},"content":[]}}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"sess-1","usage":{"input_tokens":4,"output_tokens":2},"modelUsage":{"claude-sonnet-5":{"contextWindow":200000}}}'
`), 0o755)
	var prov storage.Provider
	f := setup(t, func(provs *provider.Service) storage.Provider {
		p, err := provs.Create(context.Background(), provider.Input{Name: "CC", Kind: storage.ProviderClaudeCLI, BaseURL: bin})
		if err != nil {
			t.Fatal(err)
		}
		prov = p
		return p
	})
	ctx := context.Background()
	conv, _ := f.engine.StartConversation(ctx, f.project.ID, "")
	turn, _, err := f.engine.Send(ctx, conv.ID, "chào", nil)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, turn)
	conv, _ = f.st.Chat().GetConversation(ctx, conv.ID)
	if conv.ContextTokens != 1004 || conv.ContextWindow != 200000 {
		t.Fatalf("context = %d/%d", conv.ContextTokens, conv.ContextWindow)
	}
	var lim chat.Limits
	if ok, _ := f.st.Settings().Get(ctx, chat.LimitsKey(prov.ID), &lim); !ok || lim.Windows["five_hour"].Utilization != 0.2 {
		t.Fatalf("limits = %v %+v", ok, lim)
	}
}

// The person picks who answers in a chat (each agent has its own rights).
func TestSwitchConversationAgent(t *testing.T) {
	f := setup(t, func(provs *provider.Service) storage.Provider {
		p, _ := provs.Create(context.Background(), provider.Input{Name: "CC", Kind: storage.ProviderClaudeCLI, BaseURL: "/bin/false"})
		return p
	})
	ctx := context.Background()
	agents, _ := f.engine.Agents(ctx, f.project.ID)
	if len(agents) < 2 {
		m, _ := f.st.OrgModels().GetForRepo(ctx, f.project.ID)
		if _, err := f.st.Agents().Create(ctx, storage.Agent{OrgModelID: m.ID, Key: "dev", Name: "Dev", Tier: storage.TierWorker, ModelTier: "fast"}); err != nil {
			t.Fatal(err)
		}
		agents, _ = f.engine.Agents(ctx, f.project.ID)
	}
	conv, _ := f.engine.StartConversation(ctx, f.project.ID, agents[0].ID)
	conv.SessionID, conv.Runtime = "sess-old", "claude_cli"
	f.st.Chat().UpdateConversation(ctx, conv)
	if err := f.engine.SetAgent(ctx, conv.ID, "agt_nope"); err == nil {
		t.Fatal("an agent of another project was accepted")
	}
	if err := f.engine.SetAgent(ctx, conv.ID, agents[1].ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.Chat().GetConversation(ctx, conv.ID)
	if got.AgentID != agents[1].ID || got.AgentName != agents[1].Name { // sessions stay per agent (ADR-044)
		t.Fatalf("conversation = %+v", got)
	}
}

// toHistory names the answers of other agents (after a switch).
func TestHistoryKeepsOtherAuthors(t *testing.T) {
	h := chat.HistoryFor([]storage.Message{{Role: "user", Content: "a"}, {Role: "assistant", Content: "b", Author: "Trưởng nhóm"}, {Role: "assistant", Content: "c", Author: "Dev"}}, "Dev")
	if len(h) != 3 || h[1].Author != "Trưởng nhóm" || h[2].Author != "" {
		t.Fatalf("history = %+v", h)
	}
}

// fakeTeamClaude answers as the agent named in its system prompt, with a
// session per agent, and logs every call's args and stdin.
func fakeTeamClaude(t *testing.T, reply string) (bin, dir string) {
	dir = t.TempDir()
	bin = filepath.Join(dir, "claude")
	os.WriteFile(bin, []byte(`#!/bin/sh
n=$(ls `+dir+` | grep -c 'args$')
n=$((n+1))
echo "$*" > `+dir+`/call$n.args
cat > `+dir+`/call$n.in
who=lead
case "$*" in *"Bạn là Dev"*) who=dev;; esac
echo '{"type":"system","subtype":"init","session_id":"sess-'$who'"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"'$who': `+reply+`","session_id":"sess-'$who'","usage":{"input_tokens":1,"output_tokens":1}}'
`), 0o755)
	return bin, dir
}

func call(t *testing.T, dir string, n int) (args, stdin string) {
	a, _ := os.ReadFile(filepath.Join(dir, fmt.Sprintf("call%d.args", n)))
	i, _ := os.ReadFile(filepath.Join(dir, fmt.Sprintf("call%d.in", n)))
	return string(a), string(i)
}

// ADR-044: each agent keeps its own session in a chat; coming back it gets
// only what was said since, with who said it.
func TestEachAgentKeepsItsSession(t *testing.T) {
	bin, dir := fakeTeamClaude(t, "ok")
	f := setup(t, func(provs *provider.Service) storage.Provider {
		p, _ := provs.Create(context.Background(), provider.Input{Name: "CC", Kind: storage.ProviderClaudeCLI, BaseURL: bin})
		return p
	})
	ctx := context.Background()
	m, _ := f.st.OrgModels().GetForRepo(ctx, f.project.ID)
	dev, err := f.st.Agents().Create(ctx, storage.Agent{OrgModelID: m.ID, Key: "dev", Name: "Dev", Tier: storage.TierWorker, ModelTier: "fast"})
	if err != nil {
		t.Fatal(err)
	}
	conv, _ := f.engine.StartConversation(ctx, f.project.ID, "")
	lead := conv.AgentID
	say := func(text string) {
		t.Helper()
		turn, _, err := f.engine.Send(ctx, conv.ID, text, nil)
		if err != nil {
			t.Fatal(err)
		}
		collect(t, turn)
	}
	say("chào")
	f.engine.SetAgent(ctx, conv.ID, dev.ID)
	say("sửa đi")
	f.engine.SetAgent(ctx, conv.ID, lead)
	say("tiếp")
	args2, in2 := call(t, dir, 2)
	if strings.Contains(args2, "--resume") || !strings.Contains(in2, "Cuộc trò chuyện trước đó") {
		t.Fatalf("Dev's first turn should be a transcript:\n%s\n%s", args2, in2)
	}
	args3, in3 := call(t, dir, 3)
	if !strings.Contains(args3, "--resume sess-lead") || !strings.Contains(in3, "[Dev]") || !strings.Contains(in3, "dev: ok") || !strings.Contains(in3, "tiếp") {
		t.Fatalf("lead's turn back:\n%s\n%s", args3, in3)
	}
	members, _ := f.st.Chat().Members(ctx, conv.ID)
	if len(members) != 2 || members[1].AgentID != dev.ID || members[1].SessionID != "sess-dev" {
		t.Fatalf("members = %+v", members)
	}
}

// fakeGroupClaude: like fakeTeamClaude, replies from reply-<who> files and
// sleeps from sleep-<who> files (who = lead | dev).
func fakeGroupClaude(t *testing.T) (bin, dir string) {
	dir = t.TempDir()
	bin = filepath.Join(dir, "claude")
	os.WriteFile(bin, []byte(`#!/bin/sh
n=$(ls `+dir+` | grep -c 'args$')
n=$((n+1))
echo "$*" > `+dir+`/call$n.args
cat > `+dir+`/call$n.in
who=lead
case "$*" in *"Bạn là Dev"*) who=dev;; esac
[ -f `+dir+`/sleep-$who ] && sleep $(cat `+dir+`/sleep-$who)
reply=ok
[ -f `+dir+`/reply-$who ] && reply=$(cat `+dir+`/reply-$who)
echo '{"type":"system","subtype":"init","session_id":"sess-'$who'"}'
echo '{"type":"assistant","message":{"model":"m","usage":{"input_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},"content":[]}}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"'$who': '"$reply"'","session_id":"sess-'$who'","usage":{"input_tokens":1,"output_tokens":1}}'
`), 0o755)
	return bin, dir
}

type group struct {
	f       fixture
	dir     string
	conv    storage.Conversation
	lead    string
	dev     storage.Agent
	leadNm  string
	engine  *chat.Engine
	context context.Context
}

func newGroup(t *testing.T) group {
	bin, dir := fakeGroupClaude(t)
	f := setup(t, func(provs *provider.Service) storage.Provider {
		p, _ := provs.Create(context.Background(), provider.Input{Name: "CC", Kind: storage.ProviderClaudeCLI, BaseURL: bin})
		return p
	})
	ctx := context.Background()
	m, _ := f.st.OrgModels().GetForRepo(ctx, f.project.ID)
	dev, err := f.st.Agents().Create(ctx, storage.Agent{OrgModelID: m.ID, Key: "dev", Name: "Dev", Tier: storage.TierWorker, ModelTier: "fast"})
	if err != nil {
		t.Fatal(err)
	}
	conv, _ := f.engine.StartConversation(ctx, f.project.ID, "")
	return group{f: f, dir: dir, conv: conv, lead: conv.AgentID, leadNm: conv.AgentName, dev: dev, engine: f.engine, context: ctx}
}

// sendAll sends text and follows every turn of the chain; the replies' authors.
func (g group) sendAll(t *testing.T, text string) (authors []string) {
	t.Helper()
	turn, _, err := g.engine.Send(g.context, g.conv.ID, text, nil)
	if err != nil {
		t.Fatal(err)
	}
	for turn != nil {
		events := collect(t, turn)
		last := events[len(events)-1]
		if last.Message != nil {
			authors = append(authors, last.Message.Author)
		}
		next, _ := g.engine.Turn(last.NextTurnID)
		if last.NextTurnID == "" {
			next = nil
		}
		turn = next
	}
	return authors
}

func TestMentionPullsAgentIn(t *testing.T) { // ADR-044
	g := newGroup(t)
	if got := g.sendAll(t, "@Dev xem lỗi này"); len(got) != 1 || got[0] != "Dev" {
		t.Fatalf("authors = %v", got)
	}
	if got := g.sendAll(t, "cảm ơn"); len(got) != 1 || got[0] != g.leadNm {
		t.Fatalf("no tag → the default agent, got %v", got)
	}
	members, _ := g.f.st.Chat().Members(g.context, g.conv.ID)
	if len(members) != 2 {
		t.Fatalf("members = %+v", members)
	}
}

func TestTwoTagsAnswerInOrder(t *testing.T) {
	g := newGroup(t)
	got := g.sendAll(t, "@"+g.leadNm+" @Dev cùng xem")
	if len(got) != 2 || got[0] != g.leadNm || got[1] != "Dev" {
		t.Fatalf("authors = %v", got)
	}
	_, in2 := call(t, g.dir, 2)
	if !strings.Contains(in2, "lead: ok") || !strings.Contains(in2, "cùng xem") {
		t.Fatalf("Dev should see the lead's answer and the question:\n%s", in2)
	}
}

// waitAuthors waits until the chat has n replies (and errors), returning
// "author" for replies and "!" for notes.
func (g group) waitAuthors(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		msgs, _ := g.f.st.Chat().ListMessages(g.context, g.conv.ID)
		var out []string
		for _, m := range msgs {
			switch m.Role {
			case "assistant":
				out = append(out, m.Author)
			case "error":
				out = append(out, "!")
			}
		}
		_, busy := g.engine.Active(g.conv.ID)
		if len(out) >= n && !busy && len(g.engine.Running(g.conv.ID)) == 0 {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %v", out)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// delegateDuring hands a task to agent from inside the answer in progress,
// as the delegate tool does (the fake CLI cannot call office's MCP).
func (g group) delegateDuring(t *testing.T, agent, task string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if turn, ok := g.engine.Active(g.conv.ID); ok {
			if _, err := g.engine.Delegate(g.context, officetools.Scope{ProjectID: g.f.project.ID, ConversationID: g.conv.ID, RunRef: turn.ID, Agent: g.leadNm}, agent, task); err != nil {
				t.Fatal(err)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no answer in progress")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ADR-044: an agent hands off with the delegate tool (like a subagent); it runs
// in the background and the one who asked reports back. A plain @Name in an
// answer only mentions: it pulls nobody in.
func TestAgentHandoffRunsInBackground(t *testing.T) {
	g := newGroup(t)
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	os.WriteFile(filepath.Join(g.dir, "reply-lead"), []byte("như @Dev nói hôm qua"), 0o644)
	os.WriteFile(filepath.Join(g.dir, "reply-dev"), []byte("xong"), 0o644)
	turn, _, err := g.engine.Send(g.context, g.conv.ID, "làm việc X", nil)
	if err != nil {
		t.Fatal(err)
	}
	g.delegateDuring(t, "Dev", "sửa lỗi X trong api.go")
	collect(t, turn)
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	// lead → Dev (bg, from the tool) → lead reports; the lead's "@Dev" mention adds nothing
	got := g.waitAuthors(t, 3)
	want := []string{g.leadNm, "Dev", g.leadNm}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("authors = %v, want %v", got, want)
	}
	_, devIn := call(t, g.dir, 2)
	if !strings.Contains(devIn, "sửa lỗi X trong api.go") {
		t.Fatalf("Dev's prompt lacks the task:\n%s", devIn)
	}
	_, reportIn := call(t, g.dir, 3)
	if !strings.Contains(reportIn, "dev: xong") {
		t.Fatalf("report prompt:\n%s", reportIn)
	}
}

func TestMentionInAnswerPullsNobody(t *testing.T) {
	g := newGroup(t)
	os.WriteFile(filepath.Join(g.dir, "reply-lead"), []byte("@Dev có thể làm, bạn muốn giao không?"), 0o644)
	if got := g.sendAll(t, "ai làm được"); len(got) != 1 {
		t.Fatalf("authors = %v", got)
	}
	if got := g.waitAuthors(t, 1); len(got) != 1 {
		t.Fatalf("a mention pulled someone in: %v", got)
	}
}

func TestDelegateChecks(t *testing.T) {
	g := newGroup(t)
	sc := officetools.Scope{ProjectID: g.f.project.ID, ConversationID: g.conv.ID, RunRef: "nope", Agent: g.leadNm}
	if _, err := g.engine.Delegate(g.context, sc, "Nobody", "x"); err == nil {
		t.Fatal("an unknown agent was accepted")
	}
	if _, err := g.engine.Delegate(g.context, sc, g.leadNm, "x"); err == nil {
		t.Fatal("delegating to itself was accepted")
	}
	if _, err := g.engine.Delegate(g.context, officetools.Scope{ProjectID: g.f.project.ID, TaskID: "tsk_1", Agent: g.leadNm}, "Dev", "x"); err == nil {
		t.Fatal("delegate outside a chat was accepted")
	}
}

func TestPersonKeepsChattingWhileAgentWorks(t *testing.T) {
	g := newGroup(t)
	os.WriteFile(filepath.Join(g.dir, "sleep-dev"), []byte("2"), 0o644)
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	turn, _, err := g.engine.Send(g.context, g.conv.ID, "làm X", nil)
	if err != nil {
		t.Fatal(err)
	}
	g.delegateDuring(t, "Dev", "làm X")
	collect(t, turn)
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	os.WriteFile(filepath.Join(g.dir, "reply-lead"), []byte("đang đợi Dev"), 0o644)
	if got := g.sendAll(t, "trong lúc đợi thì sao"); len(got) != 1 || got[0] != g.leadNm {
		t.Fatalf("the person is not blocked by Dev, got %v", got)
	}
	if _, _, err := g.engine.Send(g.context, g.conv.ID, "@Dev nhanh lên", nil); !errors.Is(err, chat.ErrAgentBusy) {
		t.Fatalf("tagging a working agent: %v", err)
	}
	if running := g.engine.Running(g.conv.ID); len(running) != 1 || running[0].AgentName != "Dev" || !running[0].Background {
		t.Fatalf("running = %+v", running)
	}
	g.waitAuthors(t, 4)
}

func TestStopDropsTheQueue(t *testing.T) {
	g := newGroup(t)
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("2"), 0o644)
	turn, _, err := g.engine.Send(g.context, g.conv.ID, "@"+g.leadNm+" @Dev xem", nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	turn.Cancel()
	events := collect(t, turn)
	if last := events[len(events)-1]; last.NextTurnID != "" {
		t.Fatalf("stopped, yet next = %s", last.NextTurnID)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(g.dir, "call2.args")); err == nil {
		t.Fatal("Dev ran after Stop")
	}
}

func TestBusyWhileTheNextAnswers(t *testing.T) {
	g := newGroup(t)
	os.WriteFile(filepath.Join(g.dir, "sleep-dev"), []byte("2"), 0o644)
	turn, _, err := g.engine.Send(g.context, g.conv.ID, "@"+g.leadNm+" @Dev xem", nil)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, turn) // the lead is done, Dev is answering
	if _, _, err := g.engine.Send(g.context, g.conv.ID, "chen ngang", nil); !errors.Is(err, chat.ErrBusy) {
		t.Fatalf("err = %v, want busy", err)
	}
}

// Review I2: later turns of a chain use the chat as it is now, and never write
// back an old copy (the person switched the default agent meanwhile).
func TestLaterTurnsKeepTheChatsSettings(t *testing.T) {
	g := newGroup(t)
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	os.WriteFile(filepath.Join(g.dir, "sleep-dev"), []byte("1"), 0o644)
	turn, _, err := g.engine.Send(g.context, g.conv.ID, "làm X", nil)
	if err != nil {
		t.Fatal(err)
	}
	g.delegateDuring(t, "Dev", "làm X")
	collect(t, turn)
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	if err := g.engine.SetAgent(g.context, g.conv.ID, g.dev.ID); err != nil { // while Dev works
		t.Fatal(err)
	}
	g.waitAuthors(t, 3) // Dev, then the lead reports
	c, _ := g.f.st.Chat().GetConversation(g.context, g.conv.ID)
	if c.AgentID != g.dev.ID {
		t.Fatalf("default agent written back to %s", c.AgentName)
	}
}
