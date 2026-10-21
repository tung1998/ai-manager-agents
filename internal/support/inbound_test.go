package support

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/collections"
	"bitbucket.org/senprints/agent-office/internal/features"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

// answers like an agent that looked up the playbooks; records the prompts.
type fakeExec struct {
	mu      sync.Mutex
	prompts []string
}

func (x *fakeExec) RunChat(_ context.Context, _, _, _, prompt, _ string) (string, string, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.prompts = append(x.prompts, prompt)
	return "conv_1", "Shop xin lỗi anh, sẽ đổi size miễn phí trong 7 ngày.", nil
}

func (x *fakeExec) calls() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return len(x.prompts)
}

type env struct {
	st      storage.Store
	s       *Service
	mux     *http.ServeMux
	exec    *fakeExec
	project string
	auto    storage.Automation
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.Migrate(ctx)
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop"})
	features.Save(ctx, st, p.ID, features.Config{Packs: map[string]bool{"support": true}})
	a, _ := st.Automations().Create(ctx, storage.Automation{ProjectID: p.ID, Name: "Xử lý phàn nàn", Source: "webhook", Action: "chat", Enabled: true,
		Prompt: "Tra phương án trong playbooks rồi soạn câu trả lời cho khách."})
	exec := &fakeExec{}
	runner := trigger.New(st, exec)
	s := New(st)
	s.SetRunner(runner)
	runner.AddOnDone(s.Done)
	pass := func(h http.HandlerFunc) http.Handler { return h }
	mux := http.NewServeMux()
	s.Routes(mux, pass, pass)
	t.Cleanup(runner.Wait)
	return &env{st: st, s: s, mux: mux, exec: exec, project: p.ID, auto: a}
}

func (e *env) do(t *testing.T, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, req)
	out := map[string]any{}
	json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func (e *env) source(t *testing.T, mode string, rate int) (string, string) {
	t.Helper()
	code, body := e.do(t, "POST", "/api/projects/"+e.project+"/support/sources", "",
		map[string]any{"name": "Review web", "kind": "api", "automation_id": e.auto.ID, "mode": mode, "rate_per_min": rate})
	if code != 201 {
		t.Fatalf("create source = %d %v", code, body)
	}
	return body["source"].(map[string]any)["id"].(string), body["token"].(string)
}

// ADR-171: a complaint comes in by API, the automation finds the answer, the
// caller gets it back in the same response; sent again it is one message.
func TestComplaintAnsweredByAPI(t *testing.T) {
	e := newEnv(t)
	sid, tok := e.source(t, "auto", 30)
	complaint := map[string]any{"external_id": "review-1", "thread": "order-77", "text": "Áo giao sai size, rất thất vọng!",
		"customer": map[string]any{"name": "Nguyễn An", "email": "AN@x.vn"}, "meta": map[string]any{"rating": 1}, "wait": 10}

	code, body := e.do(t, "POST", "/hooks/support/"+sid, tok, complaint)
	if code != 200 || body["status"] != "answered" || !strings.Contains(body["reply"].(map[string]any)["text"].(string), "đổi size") {
		t.Fatalf("answer = %d %v", code, body)
	}
	if p := e.exec.prompts[0]; !strings.Contains(p, "Tra phương án") || !strings.Contains(p, "giao sai size") || !strings.Contains(p, "an@x.vn") {
		t.Fatalf("the automation reads the instruction, the message, the customer: %s", p)
	}
	// sent again: the same message, the same answer, no second run
	code, again := e.do(t, "POST", "/hooks/support/"+sid, tok, complaint)
	if code != 200 || again["message_id"] != body["message_id"] || e.exec.calls() != 1 {
		t.Fatalf("again = %d %v (%d runs)", code, again, e.exec.calls())
	}
	// the ticket as the customer sees it; the customer record
	_, tk := e.do(t, "GET", "/hooks/support/"+sid+"/tickets/"+body["ticket_id"].(string), tok, nil)
	if tk["status"] != StatusWaitingCustomer || len(tk["messages"].([]any)) != 2 {
		t.Fatalf("ticket = %v", tk)
	}
	if list, total, _ := e.s.collections.Query(context.Background(), e.project, "customers", collectionsQuery("email", "an@x.vn")); total != 1 || list[0].Data["name"] != "Nguyễn An" {
		t.Fatalf("customer = %v", list)
	}
	// the same thread goes on the same ticket
	_, next := e.do(t, "POST", "/hooks/support/"+sid, tok, map[string]any{"thread": "order-77", "text": "Bao giờ đổi?", "customer": map[string]any{"email": "an@x.vn"}, "wait": 10})
	if next["ticket_id"] != body["ticket_id"] {
		t.Fatalf("same thread, new ticket: %v", next)
	}
	// a wrong token, a pack off: nothing told
	if code, _ := e.do(t, "POST", "/hooks/support/"+sid, "nope", complaint); code != 404 {
		t.Fatalf("wrong token = %d", code)
	}
	features.Save(context.Background(), e.st, e.project, features.Config{})
	if code, _ := e.do(t, "POST", "/hooks/support/"+sid, tok, complaint); code != 404 {
		t.Fatalf("pack off = %d", code)
	}
}

// approve: the answer waits for a person, who may edit it.
func TestApproveBeforeSending(t *testing.T) {
	e := newEnv(t)
	sid, tok := e.source(t, "approve", 30)
	code, body := e.do(t, "POST", "/hooks/support/"+sid, tok, map[string]any{"text": "Hàng vỡ khi nhận", "wait": 10})
	if code != 200 || body["status"] != StatusWaitingApproval || body["reply"] != nil {
		t.Fatalf("waiting = %d %v", code, body)
	}
	_, tk := e.do(t, "GET", "/api/projects/"+e.project+"/support/tickets/"+body["ticket_id"].(string), "", nil)
	var draft string
	for _, ev := range tk["events"].([]any) {
		if m := ev.(map[string]any); m["kind"] == "ai_draft" && m["state"] == DraftPending {
			draft = m["id"].(string)
		}
	}
	if code, out := e.do(t, "POST", "/api/projects/"+e.project+"/support/drafts/"+draft+"/approve", "", map[string]any{"text": "Shop gửi lại hàng mới ngay hôm nay."}); code != 204 {
		t.Fatalf("approve = %d %v", code, out)
	}
	_, res := e.do(t, "GET", body["poll"].(string), tok, nil)
	if res["status"] != "answered" || res["reply"].(map[string]any)["text"] != "Shop gửi lại hàng mới ngay hôm nay." {
		t.Fatalf("after approve = %v", res)
	}
	if code, _ := e.do(t, "POST", "/api/projects/"+e.project+"/support/drafts/"+draft+"/approve", "", nil); code != 400 {
		t.Fatalf("approved twice = %d", code)
	}
	// a person answers by hand, then closes the ticket
	tid := body["ticket_id"].(string)
	if code, _ := e.do(t, "POST", "/api/projects/"+e.project+"/support/tickets/"+tid+"/reply", "", map[string]any{"text": "Đã gửi mã vận đơn."}); code != 204 {
		t.Fatalf("reply = %d", code)
	}
	if code, _ := e.do(t, "PATCH", "/api/projects/"+e.project+"/support/tickets/"+tid, "", map[string]any{"status": "resolved"}); code != 204 {
		t.Fatalf("resolve = %d", code)
	}
	_, tk = e.do(t, "GET", "/hooks/support/"+sid+"/tickets/"+tid, tok, nil)
	if tk["status"] != StatusResolved || len(tk["messages"].([]any)) != 3 {
		t.Fatalf("after reply and resolve = %v", tk)
	}
}

func TestRateLimitAndBadInput(t *testing.T) {
	e := newEnv(t)
	sid, tok := e.source(t, "auto", 1)
	if code, _ := e.do(t, "POST", "/hooks/support/"+sid, tok, map[string]any{"text": ""}); code != 400 {
		t.Fatalf("empty = %d", code)
	}
	e.do(t, "POST", "/hooks/support/"+sid, tok, map[string]any{"text": "một"})
	if code, _ := e.do(t, "POST", "/hooks/support/"+sid, tok, map[string]any{"text": "hai"}); code != 429 {
		t.Fatalf("over the rate = %d", code)
	}
	if code, out := e.do(t, "POST", "/api/projects/"+e.project+"/support/sources", "", map[string]any{"name": "x", "automation_id": "nope"}); code != 400 {
		t.Fatalf("no automation = %d %v", code, out)
	}
}

func collectionsQuery(field string, v any) collections.Query {
	return collections.Query{Where: []storage.Cond{{Field: field, Value: v}}}
}
