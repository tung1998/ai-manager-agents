package api_test

import (
	"context"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Where the limit alerts go is set on the AI connections page; a window past
// the threshold is in "Cần xử lý" too.
func TestLimitAlertAPI(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	ch, _ := e.st.Channels().Create(ctx, storage.Channel{ProjectID: pid, Kind: "discord", Name: "Dev", BotName: "shopbot", TokenEnc: "x"})
	_, b := do(t, admin, "GET", e.srv.URL+"/api/limit-alert", nil, nil)
	if b["threshold"] != float64(80) || len(b["bots"].([]any)) != 1 {
		t.Fatalf("defaults = %v", b)
	}
	if resp, _ := do(t, admin, "PUT", e.srv.URL+"/api/limit-alert", map[string]any{"channel_id": ch.ID, "chat_id": "c9", "threshold": 85}, nil); resp.StatusCode != 200 {
		t.Fatalf("save = %d", resp.StatusCode)
	}
	if resp, _ := do(t, admin, "PUT", e.srv.URL+"/api/limit-alert", map[string]any{"channel_id": "chn_nope", "chat_id": "c9"}, nil); resp.StatusCode != 400 {
		t.Fatalf("an unknown bot = %d", resp.StatusCode)
	}
	_, b = do(t, admin, "GET", e.srv.URL+"/api/limit-alert", nil, nil)
	if b["channel_id"] != ch.ID || b["chat_id"] != "c9" || b["threshold"] != float64(85) {
		t.Fatalf("saved = %v", b)
	}
	p, _ := e.provs.Create(ctx, provider.Input{Name: "Claude Code", Kind: storage.ProviderClaudeCLI, BaseURL: "/bin/false"})
	e.st.Settings().Set(ctx, chat.LimitsKey(p.ID), chat.Limits{Windows: map[string]chat.LimitWindow{
		"five_hour": {Utilization: 0.9, ResetsAt: time.Now().Add(time.Hour)}, "seven_day": {Utilization: 0.3, ResetsAt: time.Now().Add(48 * time.Hour)}}})
	_, b = do(t, admin, "GET", e.srv.URL+"/api/incidents", nil, nil)
	n := 0
	for _, x := range b["incidents"].([]any) {
		if it := x.(map[string]any); it["kind"] == "limit" {
			n++
			if it["link"] != "/providers" {
				t.Fatalf("incident = %v", it)
			}
		}
	}
	if n != 1 {
		t.Fatalf("limit incidents = %d in %v", n, b["incidents"])
	}
}
