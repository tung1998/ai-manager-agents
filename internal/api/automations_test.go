package api_test

import (
	"bitbucket.org/senprints/agent-office/internal/storage"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAutomationsAPI(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)

	resp, body := do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/automations", map[string]any{
		"name": "Jira", "source": "webhook", "action": "chat", "prompt": "Bug mới: {{payload.issue.key}}",
	}, nil)
	secret, _ := body["secret"].(string)
	if resp.StatusCode != 201 || secret == "" {
		t.Fatalf("create = %d %v", resp.StatusCode, body)
	}
	aid := body["automation"].(map[string]any)["id"].(string)
	if resp, body := do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/automations", map[string]any{
		"name": "x", "source": "schedule", "action": "task", "config": map[string]any{"cron": "99 * * * *"},
	}, nil); resp.StatusCode != 400 {
		t.Fatalf("bad cron = %d %v", resp.StatusCode, body)
	}
	if _, body := do(t, admin, "GET", e.srv.URL+"/api/automations/preview-schedule?cron=0+8+*+*+1-5&tz=Asia/Ho_Chi_Minh", nil, nil); len(body["next"].([]any)) != 5 {
		t.Fatalf("preview = %v", body)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/projects/"+pid+"/automations", nil)
	res, _ := admin.Do(req)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(raw), "secret_hash") || !strings.Contains(string(raw), "/hooks/"+aid) {
		t.Fatalf("list = %s", raw)
	}

	// the webhook takes the secret, and the run shows up as a job
	hook, _ := http.NewRequest("POST", e.srv.URL+"/hooks/"+aid, strings.NewReader(`{"issue":{"key":"SP-1"}}`))
	hook.Header.Set("Authorization", "Bearer "+secret)
	hres, err := http.DefaultClient.Do(hook)
	if err != nil || hres.StatusCode != 202 {
		t.Fatalf("hook = %v %v", hres, err)
	}
	_, body = do(t, admin, "GET", e.srv.URL+"/api/jobs?project="+pid+"&origin=automation", nil, nil)
	jobs := body["jobs"].([]any)
	if len(jobs) != 1 || jobs[0].(map[string]any)["automation_name"] != "Jira" {
		t.Fatalf("jobs = %v", body)
	}
	if _, body := do(t, admin, "GET", e.srv.URL+"/api/jobs/stats?project="+pid+"&by=kind", nil, nil); body["totals"] == nil {
		t.Fatalf("stats = %v", body)
	}

	// members cannot create or run
	member := e.client(t)
	login(t, e, member, "member@x.io", "member-password")
	if resp, _ := do(t, member, "POST", e.srv.URL+"/api/automations/"+aid+"/run", map[string]any{}, nil); resp.StatusCode != 403 {
		t.Fatalf("member run = %d", resp.StatusCode)
	}
}

func TestScriptAutomationAPI(t *testing.T) { // ADR-041
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	if resp, body := do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/automations", map[string]any{
		"name": "x", "source": "schedule", "action": "script", "config": map[string]any{"every_minutes": 5}, "script": map[string]any{"lang": "ruby", "body": "x"},
	}, nil); resp.StatusCode != 400 {
		t.Fatalf("unknown language = %d %v", resp.StatusCode, body)
	}
	resp, body := do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/automations", map[string]any{
		"name": "đếm", "source": "schedule", "action": "script", "config": map[string]any{"every_minutes": 60},
		"script": map[string]any{"lang": "bash", "body": "echo hi; exit 4"}, "escalate": map[string]any{"when": "never"},
	}, nil)
	if resp.StatusCode != 201 {
		t.Fatalf("create = %d %v", resp.StatusCode, body)
	}
	a := body["automation"].(map[string]any)
	if a["script"].(map[string]any)["body"] != "echo hi; exit 4" || a["escalate"].(map[string]any)["when"] != "never" {
		t.Fatalf("automation = %v", a)
	}
	_, run := do(t, admin, "POST", e.srv.URL+"/api/automations/"+a["id"].(string)+"/run", map[string]any{}, nil)
	jid := run["job"].(map[string]any)["id"].(string)
	var job map[string]any
	for i := 0; i < 50; i++ {
		_, got := do(t, admin, "GET", e.srv.URL+"/api/jobs/"+jid, nil, nil)
		job = got["job"].(map[string]any)
		if job["status"] != "pending" && job["status"] != "running" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job["kind"] != "script" || job["status"] != "failed" || job["exit_code"] != float64(4) || !strings.Contains(job["output"].(string), "hi") {
		t.Fatalf("job = %v", job)
	}
}

func TestEscalationAgentAndRetry(t *testing.T) { // I4, M2
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	_, body = do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "other"}, nil)
	oid := body["project"].(map[string]any)["id"].(string)
	m, _ := e.st.OrgModels().Create(context.Background(), storage.OrgModel{RepoID: oid, Key: "m", Name: "m", Kind: "solo"})
	foreign, _ := e.st.Agents().Create(context.Background(), storage.Agent{OrgModelID: m.ID, Key: "x", Name: "X", Tier: storage.TierLead, ModelTier: "fast"})
	if resp, body := do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/automations", map[string]any{
		"name": "x", "source": "schedule", "action": "script", "config": map[string]any{"every_minutes": 5},
		"script": map[string]any{"lang": "bash", "body": "exit 1"}, "escalate": map[string]any{"when": "failure", "action": "chat", "agent_id": foreign.ID},
	}, nil); resp.StatusCode != 400 {
		t.Fatalf("foreign escalation agent = %d %v", resp.StatusCode, body)
	}
	_, body = do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/automations", map[string]any{
		"name": "y", "source": "schedule", "action": "script", "config": map[string]any{"every_minutes": 600},
		"script": map[string]any{"lang": "bash", "body": "exit 1"}, "escalate": map[string]any{"when": "failure", "action": "chat"},
	}, nil)
	aid := body["automation"].(map[string]any)["id"].(string)
	// a child an escalation made, that failed: retrying calls the agent again, not the script
	child, _ := e.st.Jobs().Create(context.Background(), storage.Job{ProjectID: pid, Kind: "chat_turn", Origin: "automation", OriginID: aid,
		Trigger: "escalate", Status: "failed", ParentJobID: "job_parent", Payload: `{"output":"boom","exit_code":1}`})
	resp, body := do(t, admin, "POST", e.srv.URL+"/api/jobs/"+child.ID+"/retry", map[string]any{}, nil)
	if resp.StatusCode != 202 {
		t.Fatalf("retry = %d %v", resp.StatusCode, body)
	}
	j := body["job"].(map[string]any)
	if j["kind"] != "chat_turn" || j["parent_job_id"] != "job_parent" {
		t.Fatalf("retried job = %v", j)
	}
}

func TestAutomationBuilderAPI(t *testing.T) { // ADR-042
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	_, body := do(t, admin, "GET", e.srv.URL+"/api/templates", nil, nil)
	solo := ""
	for _, x := range body["templates"].([]any) {
		if m := x.(map[string]any); m["key"] == "solo" {
			solo = m["id"].(string)
		}
	}
	_, body = do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop", "template_id": solo}, nil)
	pid := body["project"].(map[string]any)["id"].(string)

	// test a script without saving
	resp, body := do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/automations/test-script", map[string]any{
		"script": map[string]any{"lang": "bash", "body": "read p; echo \"got $p\"; exit 2"}, "payload": "hi",
	}, nil)
	if resp.StatusCode != 200 || body["exit_code"] != float64(2) || !strings.Contains(body["output"].(string), "got hi") {
		t.Fatalf("test-script = %d %v", resp.StatusCode, body)
	}
	member := e.client(t)
	login(t, e, member, "member@x.io", "member-password")
	if resp, _ := do(t, member, "POST", e.srv.URL+"/api/projects/"+pid+"/automations/test-script", map[string]any{"script": map[string]any{"lang": "bash", "body": "id"}}, nil); resp.StatusCode != 403 {
		t.Fatalf("member test-script = %d", resp.StatusCode)
	}

	// a building chat, hidden from the project's chats, tied to the automation when it is saved
	_, body = do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/conversations", map[string]any{"purpose": "automation"}, nil)
	cid := body["conversation"].(map[string]any)["id"].(string)
	if _, body := do(t, admin, "GET", e.srv.URL+"/api/projects/"+pid+"/conversations", nil, nil); len(body["conversations"].([]any)) != 0 {
		t.Fatalf("building chat listed: %v", body)
	}
	_, body = do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/automations", map[string]any{
		"name": "đếm", "source": "schedule", "action": "script", "config": map[string]any{"every_minutes": 60},
		"script": map[string]any{"lang": "bash", "body": "echo ok"}, "conversation_id": cid,
	}, nil)
	aid := body["automation"].(map[string]any)["id"].(string)
	resp, body = do(t, admin, "POST", e.srv.URL+"/api/automations/"+aid+"/conversation", map[string]any{}, nil)
	if resp.StatusCode != 200 || body["conversation"].(map[string]any)["id"] != cid {
		t.Fatalf("automation conversation = %d %v", resp.StatusCode, body)
	}
}
