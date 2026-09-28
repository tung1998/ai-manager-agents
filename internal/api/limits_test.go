package api_test

import (
	"context"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
)

// The latest usage windows of each connection, for the sidebar and chat.
func TestProviderLimits(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	_, body := do(t, admin, "POST", e.srv.URL+"/api/providers", map[string]any{"name": "CC", "kind": "claude_cli"}, nil)
	pid := body["provider"].(map[string]any)["id"].(string)
	reset := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	e.st.Settings().Set(context.Background(), chat.LimitsKey(pid), chat.Limits{Status: "allowed", UpdatedAt: time.Now().UTC(),
		Windows: map[string]chat.LimitWindow{"five_hour": {Utilization: 0.19, ResetsAt: reset}}})
	member := e.client(t)
	login(t, e, member, "member@x.io", "member-password")
	resp, body := do(t, member, "GET", e.srv.URL+"/api/providers/limits", nil, nil)
	list, _ := body["limits"].([]any)
	if resp.StatusCode != 200 || len(list) != 1 {
		t.Fatalf("limits = %d %v", resp.StatusCode, body)
	}
	l := list[0].(map[string]any)
	w := l["windows"].(map[string]any)["five_hour"].(map[string]any)
	if l["provider_id"] != pid || l["provider_name"] != "CC" || l["is_default"] != true || w["utilization"] != 0.19 {
		t.Fatalf("entry = %v", l)
	}
}
