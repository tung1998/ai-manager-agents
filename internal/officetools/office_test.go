package officetools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// The office assistant's scope (ADR-046): tools across projects, a project
// named by id or name, figures, and hand-offs to a project's chat.
func TestOfficeScopeTools(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	office, _ := st.Repos().Create(ctx, storage.Repo{Name: "Office"})
	shop, _ := st.Repos().Create(ctx, storage.Repo{Name: "Storefront", Description: "cửa hàng"})
	st.Jobs().Create(ctx, storage.Job{ProjectID: shop.ID, Kind: "chat_turn", Origin: "user", Trigger: "ui", Title: "hỏi graylog", Status: "done"})
	acts := actions.New(st, nil)
	acts.SetRunner(fakeRunner{})
	box := New(st, nil, acts)
	box.SetOffice(func(ctx context.Context) string { return office.ID })
	sc := Scope{ProjectID: office.ID, Office: true, Agent: "Trợ lý office"}
	call := func(name string, args map[string]any) (string, bool) {
		raw, _ := json.Marshal(args)
		return box.Call(ctx, sc, name, raw)
	}
	names := map[string]bool{}
	for _, tl := range box.ToolsFor(sc) {
		names[tl.Name] = true
	}
	for _, n := range []string{"projects", "jobs_query", "usage_summary", "handoff", "run_automation"} {
		if !names[n] {
			t.Errorf("office scope lacks %s", n)
		}
	}
	if project := box.ToolsFor(Scope{ProjectID: shop.ID}); len(project) > 0 {
		for _, tl := range project {
			if tl.Name == "projects" || tl.Name == "handoff" {
				t.Errorf("a project's agent got %s", tl.Name)
			}
		}
	}
	if out, isErr := call("projects", nil); isErr || !strings.Contains(out, "Storefront") || strings.Contains(out, `"Office"`) {
		t.Fatalf("projects = %v %s", isErr, out)
	}
	if out, isErr := call("jobs_query", map[string]any{"project": "storefront"}); isErr || !strings.Contains(out, "hỏi graylog") {
		t.Fatalf("jobs_query = %v %s", isErr, out)
	}
	if out, isErr := call("handoff", map[string]any{"project": "Storefront", "message": "sửa lỗi 500"}); isErr || !strings.Contains(out, "/projects/"+shop.ID+"?tab=chat&draft=") {
		t.Fatalf("handoff = %v %s", isErr, out)
	}
	if _, isErr := call("jobs_query", map[string]any{"project": "không có"}); !isErr {
		t.Fatal("an unknown project was accepted")
	}
	// review I1: the assistant's own chats (each person's) never show through tools
	private, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: office.ID, Title: "riêng", CreatedBy: "human:b@x.io"})
	st.Chat().AddMessage(ctx, storage.Message{ConversationID: private.ID, Role: "user", Content: "lương của tôi"})
	st.Jobs().Create(ctx, storage.Job{ProjectID: office.ID, Kind: "chat_turn", Origin: "user", Trigger: "ui", Title: "lương của tôi", Status: "done", ConversationID: private.ID})
	if out, _ := call("jobs_query", nil); strings.Contains(out, "lương của tôi") {
		t.Fatalf("jobs_query shows an assistant chat: %s", out)
	}
	if _, isErr := call("read_link", map[string]any{"url": "http://x/projects/" + office.ID + "?tab=chat&c=" + private.ID}); !isErr {
		t.Fatal("read_link read another person's assistant chat")
	}
}

// jobs_query excludes the assistant's own jobs at the database query, so a
// limited page is not left short by them crowding out other projects' jobs.
func TestJobsQueryExcludesOwnBeforeLimit(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	office, _ := st.Repos().Create(ctx, storage.Repo{Name: "Office"})
	shop, _ := st.Repos().Create(ctx, storage.Repo{Name: "Storefront"})
	// Storefront's jobs first (older), the assistant's own jobs after (newer,
	// so they sort first by created_at/id desc and would crowd out the page
	// under the old post-query filter).
	for i := 0; i < 10; i++ {
		st.Jobs().Create(ctx, storage.Job{ProjectID: shop.ID, Kind: "chat_turn", Origin: "user", Trigger: "ui", Title: "việc của shop", Status: "done"})
	}
	for i := 0; i < 25; i++ {
		st.Jobs().Create(ctx, storage.Job{ProjectID: office.ID, Kind: "chat_turn", Origin: "user", Trigger: "ui", Title: "riêng của trợ lý", Status: "done"})
	}
	acts := actions.New(st, nil)
	acts.SetRunner(fakeRunner{})
	box := New(st, nil, acts)
	box.SetOffice(func(ctx context.Context) string { return office.ID })
	sc := Scope{ProjectID: office.ID, Office: true, Agent: "Trợ lý office"}
	raw, _ := json.Marshal(map[string]any{"limit": 10})
	out, isErr := box.Call(ctx, sc, "jobs_query", raw)
	if isErr {
		t.Fatalf("jobs_query failed: %s", out)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(rows) != 10 {
		t.Fatalf("jobs_query returned %d rows, want 10 (own-project jobs should not crowd out the page): %s", len(rows), out)
	}
	for _, r := range rows {
		if r["project_id"] == office.ID {
			t.Fatalf("jobs_query returned an assistant's own job: %v", r)
		}
	}
}

// ReadOnly must be set deliberately: only tools that never change state may
// have it, everything else (including new tools, by Go's zero value) stays
// false so an MCP client never auto-runs a write without asking.
func TestToolsReadOnly(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	office, _ := st.Repos().Create(ctx, storage.Repo{Name: "Office"})
	acts := actions.New(st, nil)
	acts.SetRunner(fakeRunner{})
	box := New(st, nil, acts)
	box.SetOffice(func(ctx context.Context) string { return office.ID })
	box.SetConfig(fakeConfig{})
	box.SetBurn(func(context.Context, Scope, string, BurnInput) (string, error) { return "", nil })
	box.SetDelegate(func(context.Context, Scope, string, string) (string, error) { return "", nil })
	box.SetSendFile(func(context.Context, Scope, string, string) (string, error) { return "", nil })
	box.SetWorkflow(fakeWorkflows{})

	readOnly := map[string]bool{
		"projects": true, "jobs_query": true, "usage_summary": true, "handoff": true,
		"ops_overview": true, "process_logs": true, "container_logs": true, "monitor_detail": true,
		"git_status": true, "git_diff": true, "git_log": true,
		"describe": true, "list": true, "get": true,
		"search_history": true, "read_link": true, "burn_list": true, "recall": true,
	}
	writes := []string{"run_command", "propose_action", "propose_automation", "propose_change", "remember",
		"send_to_chat", "send_file", "delegate", "run_automation", "burn_add", "burn_pick", "burn_skip",
		"burn_done", "burn_scan_done", "burn_fail", "workflow_delegate", "workflow_send",
		"workflow_ask", "workflow_vote", "workflow_gate", "workflow_done"}

	// No single scope offers every tool at once (the office assistant gets
	// projects/handoff/run_automation but not delegate; a Burn conversation
	// gets burn_* but not send_file, which needs a bot channel): union across
	// the scopes that between them unlock every tool, like a real agent would
	// see one at a time.
	burnConv, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: office.ID, Purpose: "burn"})
	channelConv, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: office.ID, Purpose: "channel"})
	scopes := []Scope{
		{ProjectID: office.ID, Office: true, Level: "propose", ConversationID: burnConv.ID},
		{ProjectID: office.ID, Level: "propose", ConversationID: channelConv.ID},
	}

	seen := map[string]bool{}
	for _, sc := range scopes {
		for _, tl := range box.ToolsFor(sc) {
			seen[tl.Name] = true
			want := readOnly[tl.Name]
			if tl.ReadOnly != want {
				t.Errorf("tool %q: ReadOnly=%v, want %v", tl.Name, tl.ReadOnly, want)
			}
		}
	}
	for name := range readOnly {
		if !seen[name] {
			t.Errorf("expected read-only tool %q was not offered by any scope checked", name)
		}
	}
	for _, name := range writes {
		if !seen[name] {
			t.Errorf("expected write tool %q was not offered by any scope checked", name)
		}
	}
}

type fakeWorkflows struct{}

func (fakeWorkflows) WorkflowScope(Scope) (coordinator, inRun bool) { return true, false }
func (fakeWorkflows) WorkflowCall(context.Context, Scope, string, json.RawMessage) (string, error) {
	return "", nil
}

type fakeRunner struct{}

func (fakeRunner) RunAutomation(context.Context, string) (string, error) { return "job_x", nil }

type fakeConfig struct{}

func (fakeConfig) DescribeConfig(string) (string, error)                             { return "", nil }
func (fakeConfig) ListConfig(context.Context, string, string) (string, error)        { return "", nil }
func (fakeConfig) GetConfig(context.Context, string, string, string) (string, error) { return "", nil }

// A read-only agent of a project may look at settings but not propose a
// change (the office assistant may: its cards are what it is for).
func TestProposeChangeNeedsProposeLevel(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	shop, _ := st.Repos().Create(ctx, storage.Repo{Name: "Storefront"})
	box := New(st, nil, actions.New(st, nil))
	box.SetConfig(fakeConfig{})
	raw, _ := json.Marshal(map[string]any{"resource": "automation", "op": "create", "patch": map[string]any{"name": "x"}, "reason": "r"})
	out, isErr := box.Call(ctx, Scope{ProjectID: shop.ID, Level: "read"}, "propose_change", raw)
	if !isErr || !strings.Contains(out, "quyền") {
		t.Fatalf("a read-only agent proposed a change: %v %s", isErr, out)
	}
}

// Answer only: the assistant reads and answers, it proposes nothing.
func TestAnswerOnlyHasNoProposals(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	office, _ := st.Repos().Create(ctx, storage.Repo{Name: "Office"})
	acts := actions.New(st, nil)
	acts.SetRunner(fakeRunner{})
	box := New(st, nil, acts)
	box.SetOffice(func(ctx context.Context) string { return office.ID })
	sc := Scope{ProjectID: office.ID, Office: true, Agent: "Trợ lý office", AnswerOnly: true}
	for _, tl := range box.ToolsFor(sc) {
		if strings.HasPrefix(tl.Name, "propose") || tl.Name == "run_automation" {
			t.Fatalf("answer only offers %s", tl.Name)
		}
	}
	if !box.Has(sc, "jobs_query") {
		t.Fatal("answer only still reads the office")
	}
}

// ADR-086: search_history finds the project's chats by their words, with a
// link to read them.
func TestSearchHistory(t *testing.T) {
	ctx := context.Background()
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop"})
	tb := New(st, nil, nil)
	c, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p.ID, Title: "Thanh toán", CreatedBy: "human:a@x.io"})
	m, _ := st.Chat().AddMessage(ctx, storage.Message{ConversationID: c.ID, Role: "user", Author: "human:a@x.io", Content: "sửa lỗi thanh toán PayPal"})
	out, isErr := tb.Call(ctx, Scope{ProjectID: p.ID, ConversationID: c.ID}, "search_history", []byte(`{"query":"thanh toan paypal"}`))
	if isErr || !strings.Contains(out, "Thanh toán") || !strings.Contains(out, "&m="+m.ID) {
		t.Fatalf("search = %v %s", isErr, out)
	}
}
