package api_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
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
