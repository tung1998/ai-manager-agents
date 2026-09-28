package api_test

import (
	"context"
	"strings"
	"testing"
)

// ADR-048: channels are set up per project; the bot token is written, never read back.
func TestChannelsAPI(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	_, body := do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	if resp, _ := do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/channels", map[string]any{"kind": "slack", "name": "x", "token": "t"}, nil); resp.StatusCode != 400 {
		t.Fatalf("unknown kind = %d", resp.StatusCode)
	}
	if resp, _ := do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/channels", map[string]any{"kind": "telegram", "name": "x", "allow": []string{"*"}}, nil); resp.StatusCode != 400 {
		t.Fatalf("no token = %d", resp.StatusCode)
	}
	// review C2: an enabled channel names who may use it ("*" = anyone, on purpose)
	if resp, _ := do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/channels", map[string]any{"kind": "telegram", "name": "x", "token": "t"}, nil); resp.StatusCode != 400 {
		t.Fatalf("an open channel without an allow list = %d", resp.StatusCode)
	}
	resp, body := do(t, admin, "POST", e.srv.URL+"/api/projects/"+pid+"/channels", map[string]any{
		"kind": "telegram", "name": "Hỗ trợ", "token": "123:secret-bot-token", "scope": "đơn hàng", "filter_enabled": true, "allow": []string{"42"}}, nil)
	if resp.StatusCode != 201 {
		t.Fatalf("create = %d %v", resp.StatusCode, body)
	}
	ch := body["channel"].(map[string]any)
	id := ch["id"].(string)
	if ch["has_token"] != true || strings.Contains(mustJSON(body), "secret-bot-token") || ch["mode"] != "read" {
		t.Fatalf("channel = %v", ch)
	}
	stored, _ := e.st.Channels().Get(context.Background(), id)
	if stored.TokenEnc == "" || strings.Contains(stored.TokenEnc, "secret-bot-token") {
		t.Fatalf("token stored in clear: %q", stored.TokenEnc)
	}
	resp, body = do(t, admin, "PATCH", e.srv.URL+"/api/channels/"+id, map[string]any{"name": "Hỗ trợ 2", "enabled": false}, nil)
	if resp.StatusCode != 200 || body["channel"].(map[string]any)["name"] != "Hỗ trợ 2" {
		t.Fatalf("patch = %d %v", resp.StatusCode, body)
	}
	if again, _ := e.st.Channels().Get(context.Background(), id); again.TokenEnc != stored.TokenEnc {
		t.Fatal("a patch without token changed the token")
	}
	_, body = do(t, admin, "GET", e.srv.URL+"/api/projects/"+pid+"/channels", nil, nil)
	if len(body["channels"].([]any)) != 1 {
		t.Fatalf("list = %v", body)
	}
	member := e.client(t)
	login(t, e, member, "member@x.io", "member-password")
	if resp, _ := do(t, member, "GET", e.srv.URL+"/api/projects/"+pid+"/channels", nil, nil); resp.StatusCode != 403 {
		t.Fatalf("member list = %d", resp.StatusCode)
	}
	if resp, _ := do(t, admin, "DELETE", e.srv.URL+"/api/channels/"+id, nil, nil); resp.StatusCode != 204 {
		t.Fatalf("delete = %d", resp.StatusCode)
	}
}
