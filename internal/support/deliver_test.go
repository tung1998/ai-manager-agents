package support

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/channels"
	"bitbucket.org/senprints/agent-office/internal/officetools"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// plainBox "seals" by a prefix: enough to see a secret is kept and opened.
type plainBox struct{}

func (plainBox) Seal(p string) (string, error) { return "sealed:" + p, nil }
func (plainBox) Open(s string) (string, error) { return strings.TrimPrefix(s, "sealed:"), nil }

// receiver records the requests a hook or Crisp sends.
type receiver struct {
	mu   sync.Mutex
	reqs []*http.Request
	body []string
	fail int // answer 500 this many times first
}

func (x *receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.fail > 0 {
		x.fail--
		w.WriteHeader(500)
		return
	}
	x.reqs, x.body = append(x.reqs, r), append(x.body, string(b))
}

func (x *receiver) got() []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	return append([]string(nil), x.body...)
}

func (e *env) settle(t *testing.T, until func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !until() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		e.s.Deliver(context.Background(), time.Now().UTC())
		time.Sleep(30 * time.Millisecond)
	}
}

// ADR-174: hooks hear a ticket's events, signed; a failing one is tried again.
func TestHooksSignedAndRetried(t *testing.T) {
	e := newEnv(t)
	e.s.SetBox(plainBox{})
	rec := &receiver{fail: 1}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	code, body := e.do(t, "POST", "/api/projects/"+e.project+"/support/hooks", "", map[string]any{"url": srv.URL, "events": []string{"ticket.created", "ticket.replied"}})
	if code != 201 || body["secret"] == "" {
		t.Fatalf("hook = %d %v", code, body)
	}
	secret := body["secret"].(string)
	sid, tok := e.source(t, "auto", 30)
	e.do(t, "POST", "/hooks/support/"+sid, tok, map[string]any{"text": "Áo bị rách", "wait": 10})
	now := time.Now().UTC()
	e.s.Deliver(context.Background(), now) // the first try of the first fails
	if len(rec.got()) != 1 {
		t.Fatalf("after one round = %v", rec.got())
	}
	e.s.Deliver(context.Background(), now.Add(time.Minute)) // after its wait: both go
	got := rec.got()
	if len(got) != 2 || !strings.Contains(strings.Join(got, ""), `"event":"ticket.replied"`) || !strings.Contains(strings.Join(got, ""), `"event":"ticket.created"`) {
		t.Fatalf("hooks = %v", got)
	}
	for i, r := range rec.reqs {
		if r.Header.Get("X-Office-Signature") != "sha256="+Sign(secret, []byte(got[i])) {
			t.Fatalf("signature of %d", i)
		}
	}
	_, del := e.do(t, "GET", "/api/projects/"+e.project+"/support/deliveries", "", nil)
	if n := len(del["deliveries"].([]any)); n != 2 {
		t.Fatalf("deliveries = %d", n)
	}
}

// Crisp: a visitor's message in by Crisp's web hook, the answer back by
// Crisp's API, as an operator.
func TestCrispInAndOut(t *testing.T) {
	e := newEnv(t)
	e.s.SetBox(plainBox{})
	crisp := &receiver{}
	srv := httptest.NewServer(crisp)
	defer srv.Close()
	CrispAPI = srv.URL
	code, body := e.do(t, "POST", "/api/projects/"+e.project+"/support/sources", "", map[string]any{"name": "Crisp", "kind": "crisp", "automation_id": e.auto.ID,
		"mode": "auto", "secret": "crisp-key", "config": map[string]any{"website_id": "web1", "identifier": "ident"}})
	if code != 201 {
		t.Fatalf("crisp source = %d %v", code, body)
	}
	sid, tok := body["source"].(map[string]any)["id"].(string), body["token"].(string)
	ev := map[string]any{"event": "message:send", "data": map[string]any{"session_id": "session_9", "type": "text", "from": "user",
		"content": "Đơn của mình đâu rồi?", "fingerprint": 123, "user": map[string]any{"nickname": "Bình", "user_id": "u1"}}}
	if code, out := e.do(t, "POST", "/hooks/support/"+sid+"?token="+url.QueryEscape(tok), "", ev); code != 202 {
		t.Fatalf("crisp in = %d %v", code, out)
	}
	op := map[string]any{"event": "message:send", "data": map[string]any{"session_id": "session_9", "type": "text", "from": "operator", "content": "…"}}
	if _, out := e.do(t, "POST", "/hooks/support/"+sid, tok, op); out["status"] != "ignored" {
		t.Fatalf("an operator's message = %v", out)
	}
	e.settle(t, func() bool { return len(crisp.got()) == 1 })
	r := crisp.reqs[0]
	if u, p, _ := r.BasicAuth(); u != "ident" || p != "crisp-key" || r.URL.Path != "/v1/website/web1/conversation/session_9/message" ||
		!strings.Contains(crisp.got()[0], "đổi size") || !strings.Contains(crisp.got()[0], `"from":"operator"`) {
		t.Fatalf("crisp out = %s %s %v", r.URL.Path, crisp.got()[0], r.Header)
	}
}

// A web form (urlencoded) back to its page; the answer by email.
func TestFormReplyByEmail(t *testing.T) {
	e := newEnv(t)
	e.s.SetBox(plainBox{})
	var mu sync.Mutex
	var sent []string
	SendMail = func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, addr+"|"+from+"|"+strings.Join(to, ",")+"|"+string(msg))
		return nil
	}
	code, body := e.do(t, "POST", "/api/projects/"+e.project+"/support/sources", "", map[string]any{"name": "Form", "kind": "form", "automation_id": e.auto.ID,
		"mode": "auto", "secret": "pw", "config": map[string]any{"reply_via": "email", "smtp_host": "smtp.x.vn", "smtp_from": "shop@x.vn", "smtp_user": "shop"}})
	if code != 201 {
		t.Fatalf("form source = %d %v", code, body)
	}
	sid, tok := body["source"].(map[string]any)["id"].(string), body["token"].(string)
	form := url.Values{"name": {"Chi"}, "email": {"chi@x.vn"}, "message": {"Mực in bị nhòe"}, "order": {"#90"}, "redirect": {"https://shop.x.vn/cam-on"}}
	req := httptest.NewRequest("POST", "/hooks/support/"+sid+"?token="+url.QueryEscape(tok), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, req)
	if w.Code != 303 || w.Header().Get("Location") != "https://shop.x.vn/cam-on" {
		t.Fatalf("form = %d %v", w.Code, w.Header())
	}
	e.settle(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(sent) == 1 })
	if m := sent[0]; !strings.HasPrefix(m, "smtp.x.vn:587|shop@x.vn|chi@x.vn|") || !strings.Contains(m, "đổi size") {
		t.Fatalf("mail = %s", m)
	}
	list, _ := e.st.Support().Tickets(context.Background(), e.project, "", 10)
	evs, _ := e.st.Support().Events(context.Background(), list[0].ID)
	if evs[0].Meta["order"] != "#90" {
		t.Fatalf("form fields kept: %v", evs[0].Meta)
	}
	e.settle(t, func() bool {
		evs, _ := e.st.Support().Events(context.Background(), list[0].ID)
		return evs[len(evs)-1].Kind == "sent" && evs[len(evs)-1].State == "sent"
	})
}

// A bot the pack listens to: its message is a ticket, the answer goes back to its chat.
func TestBotSource(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ch, _ := e.st.Channels().Create(ctx, storage.Channel{ProjectID: e.project, Kind: "telegram", Name: "Shop bot", Enabled: true})
	var mu sync.Mutex
	var out []string
	e.s.SetBotSender(func(_ context.Context, channelID, chatID, text string) error {
		mu.Lock()
		defer mu.Unlock()
		out = append(out, channelID+"|"+chatID+"|"+text)
		return nil
	})
	if code, body := e.do(t, "POST", "/api/projects/"+e.project+"/support/sources", "", map[string]any{"name": "Bot", "kind": "bot", "automation_id": e.auto.ID,
		"mode": "auto", "config": map[string]any{"channel_id": ch.ID}}); code != 201 {
		t.Fatalf("bot source = %d %v", code, body)
	}
	other, _ := e.st.Channels().Create(ctx, storage.Channel{ProjectID: e.project, Kind: "telegram", Name: "Other", Enabled: true})
	if e.s.Take(ctx, other, incoming("42", "hello")) {
		t.Fatal("a bot no source listens to: left to its rules")
	}
	if !e.s.Take(ctx, ch, incoming("42", "Áo giao sai màu")) {
		t.Fatal("the source's bot: taken")
	}
	e.settle(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(out) == 1 })
	if out[0] != ch.ID+"|42|Shop xin lỗi anh, sẽ đổi size miễn phí trong 7 ngày." {
		t.Fatalf("bot out = %v", out)
	}
}

// Case types: words choose the case, the case how its answer goes; a
// customer asking for a person gets no AI answer.
func TestCasesAndHuman(t *testing.T) {
	e := newEnv(t)
	if code, out := e.do(t, "PUT", "/api/projects/"+e.project+"/support/settings", "", map[string]any{"sla_minutes": 30,
		"cases": []map[string]any{{"key": "refund", "name": "Hoàn tiền", "keywords": []string{"hoàn tiền"}, "mode": "approve", "sla_minutes": 10}}}); code != 200 {
		t.Fatalf("settings = %d %v", code, out)
	}
	sid, tok := e.source(t, "auto", 30)
	_, res := e.do(t, "POST", "/hooks/support/"+sid, tok, map[string]any{"text": "Mình muốn HOAN TIEN đơn này", "wait": 10})
	if res["status"] != StatusWaitingApproval {
		t.Fatalf("refund (approve) = %v", res)
	}
	tk, _ := e.st.Support().Ticket(context.Background(), res["ticket_id"].(string))
	if tk.CaseType != "refund" || tk.SLADueAt == nil || (tk.SLADueAt.Sub(tk.CreatedAt)-10*time.Minute).Abs() > time.Second {
		t.Fatalf("ticket = %+v", tk)
	}
	runs := e.exec.calls()
	_, res = e.do(t, "POST", "/hooks/support/"+sid, tok, map[string]any{"text": "Cho mình gặp nhân viên", "thread": "t9"})
	if res["status"] != "human" || e.exec.calls() != runs {
		t.Fatalf("a person asked = %v (%d runs)", res, e.exec.calls())
	}
	_, res = e.do(t, "POST", "/hooks/support/"+sid, tok, map[string]any{"text": "Sao lâu vậy?", "thread": "t9"})
	if res["status"] != "human" || e.exec.calls() != runs {
		t.Fatalf("the ticket stays a person's = %v", res)
	}
	if code, _ := e.do(t, "PUT", "/api/projects/"+e.project+"/support/settings", "", map[string]any{"cases": []map[string]any{{"key": "x", "mode": "magic"}}}); code != 400 {
		t.Fatalf("a bad mode = %d", code)
	}
}

// SLA: a ticket past its first-answer deadline is told once; the report sums up.
func TestSLAAndReport(t *testing.T) {
	e := newEnv(t)
	e.s.SetBox(plainBox{})
	rec := &receiver{}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	e.do(t, "POST", "/api/projects/"+e.project+"/support/hooks", "", map[string]any{"url": srv.URL, "events": []string{"ticket.sla_breached"}})
	e.do(t, "PUT", "/api/projects/"+e.project+"/support/settings", "", map[string]any{"sla_minutes": 5,
		"cases": []map[string]any{{"key": "people", "keywords": []string{"khiếu nại"}, "mode": "human"}}})
	sid, tok := e.source(t, "auto", 30)
	e.do(t, "POST", "/hooks/support/"+sid, tok, map[string]any{"text": "Khiếu nại về shipper"})
	e.do(t, "POST", "/hooks/support/"+sid, tok, map[string]any{"text": "Áo sai size", "wait": 10})
	later := time.Now().UTC().Add(10 * time.Minute)
	e.s.Deliver(context.Background(), later)
	e.s.Deliver(context.Background(), later.Add(2*time.Minute))
	if got := rec.got(); len(got) != 1 || !strings.Contains(got[0], "Khiếu nại") {
		t.Fatalf("sla hooks = %v", got)
	}
	_, rep := e.do(t, "GET", "/api/projects/"+e.project+"/support/report?days=7", "", nil)
	if rep["tickets"].(float64) != 2 || rep["answered"].(float64) != 1 || rep["sla_breached"].(float64) != 1 ||
		rep["escalated"].(float64) != 1 || rep["ai_auto"].(float64) != 1 || len(rep["unanswered"].([]any)) != 1 {
		b, _ := json.Marshal(rep)
		t.Fatalf("report = %s", b)
	}
}

// The agent's tools: playbooks found with their source; a note, a case.
func TestAgentTools(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.s.On(ctx, e.project) // the pack's collections made
	e.s.SaveSettings(ctx, e.project, Settings{Cases: []CaseType{{Key: "wrong_size"}}})
	e.s.collections.Upsert(ctx, e.project, "playbooks", "", map[string]any{"title": "Giao sai size", "kind": "macro", "case_type": "wrong_size",
		"content": "Đổi size miễn phí trong 7 ngày", "enabled": true}, "x")
	e.s.collections.Upsert(ctx, e.project, "playbooks", "", map[string]any{"title": "Cũ", "kind": "macro", "content": "đổi size mất phí", "enabled": false}, "x")
	read, write := officetools.Scope{ProjectID: e.project, Level: "read"}, officetools.Scope{ProjectID: e.project, Level: "propose", Agent: "cskh"}
	if n := len(e.s.Tools(read)); n != 3 {
		t.Fatalf("read tools = %d", n)
	}
	out, isErr := e.s.Call(ctx, read, "support_kb_search", []byte(`{"query":"doi size"}`))
	if isErr || !strings.Contains(out, "Giao sai size") || strings.Contains(out, "mất phí") {
		t.Fatalf("kb = %s", out)
	}
	sid, tok := e.source(t, "approve", 30)
	_, res := e.do(t, "POST", "/hooks/support/"+sid, tok, map[string]any{"text": "Sai size", "wait": 10})
	tid := res["ticket_id"].(string)
	if _, isErr := e.s.Call(ctx, read, "support_note", []byte(`{"ticket":"`+tid+`","text":"x"}`)); !isErr {
		t.Fatal("read level wrote a note")
	}
	if out, isErr := e.s.Call(ctx, write, "support_set", []byte(`{"ticket":"`+tid+`","case_type":"wrong_size","tags":["vip"]}`)); isErr {
		t.Fatal(out)
	}
	e.s.Call(ctx, write, "support_note", []byte(`{"ticket":"`+tid+`","text":"Khách quen"}`))
	out, _ = e.s.Call(ctx, read, "support_ticket_get", []byte(`{"ticket":"`+tid+`"}`))
	if !strings.Contains(out, `"case_type":"wrong_size"`) || !strings.Contains(out, "Khách quen") || !strings.Contains(out, "vip") {
		t.Fatalf("ticket = %s", out)
	}
}

func incoming(chat, text string) channels.Incoming {
	return channels.Incoming{ChatID: chat, UserID: "7", UserName: "an", Text: text, MessageID: text, Private: true, Addressed: true}
}
