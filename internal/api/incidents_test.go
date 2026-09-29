package api_test

import (
	"context"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// "Sự cố": what needs a person across projects, each with where to fix it.
func TestIncidents(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	m, _ := e.st.Monitors().Create(ctx, storage.Monitor{ProjectID: pid, Name: "API", Type: "http", Target: "http://x", Enabled: true})
	m.Status, m.LastMessage = "down", "HTTP 500"
	e.st.Monitors().SaveStatus(ctx, m)
	e.st.Automations().Create(ctx, storage.Automation{ProjectID: pid, Name: "Báo cáo", Source: "schedule", Action: "script", DisabledCode: "failures", DisabledReason: "script lỗi"})
	ch, _ := e.st.Channels().Create(ctx, storage.Channel{ProjectID: pid, Kind: "discord", Name: "Bot", Enabled: true, TokenEnc: "x", Allow: []string{"*"}})
	e.st.Channels().SetStatus(ctx, ch.ID, "bot", "discord: token không hợp lệ", nil)
	now := time.Now().UTC()
	e.st.Jobs().Create(ctx, storage.Job{ProjectID: pid, Kind: "task", Origin: "user", Status: "failed", Error: "x", Title: "Sửa lỗi", FinishedAt: &now})

	_, b := do(t, admin, "GET", e.srv.URL+"/api/incidents", nil, nil)
	kinds := map[string]bool{}
	for _, x := range b["incidents"].([]any) {
		it := x.(map[string]any)
		kinds[it["kind"].(string)] = true
		if it["project_id"] != pid || it["link"] == "" || it["title"] == "" {
			t.Errorf("incident = %v", it)
		}
	}
	for _, k := range []string{"monitor", "automation", "bot", "jobs"} {
		if !kinds[k] {
			t.Errorf("no %s incident in %v", k, b["incidents"])
		}
	}
	if b["count"] != float64(len(b["incidents"].([]any))) {
		t.Errorf("count = %v", b["count"])
	}
}
