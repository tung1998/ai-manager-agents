package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/assistant"
	"bitbucket.org/senprints/agent-office/internal/orgmodel"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// ADR-047: a personal token lets the person's own Claude Code CLI use the
// office tools in office scope; proposals wait in the approval inbox.
func TestPersonalTokenForCLI(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	if _, err := assistant.Ensure(ctx, e.st, orgmodel.NewService(e.st), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	resp, body := do(t, admin, "POST", e.srv.URL+"/api/me/tokens", map[string]any{"name": "laptop"}, nil)
	tok, _ := body["token"].(string)
	if resp.StatusCode != 201 || !strings.HasPrefix(tok, "ofc_") {
		t.Fatalf("create = %d %v", resp.StatusCode, body)
	}
	_, body = do(t, admin, "GET", e.srv.URL+"/api/me/tokens", nil, nil)
	list := body["tokens"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["name"] != "laptop" || strings.Contains(mustJSON(list), tok) {
		t.Fatalf("list = %v", list)
	}
	id := list[0].(map[string]any)["id"].(string)

	mcp := func(token, method string) (int, map[string]any) {
		req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"`+method+`"}`)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	code, out := mcp(tok, "tools/list")
	if code != 200 || !strings.Contains(mustJSON(out), `"projects"`) {
		t.Fatalf("tools/list = %d %v", code, out)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"projects","arguments":{}}}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	res, _ := http.DefaultClient.Do(req)
	var called map[string]any
	json.NewDecoder(res.Body).Decode(&called)
	res.Body.Close()
	if called["error"] != nil {
		t.Fatalf("tools/call projects = %v", called)
	}
	if code, _ := mcp("ofc_wrong", "tools/list"); code != 401 {
		t.Fatalf("a wrong token = %d", code)
	}
	do(t, admin, "DELETE", e.srv.URL+"/api/me/tokens/"+id, nil, nil)
	if code, _ := mcp(tok, "tools/list"); code != 401 {
		t.Fatalf("a revoked token = %d", code)
	}

	// proposals without a chat (the CLI) wait in the inbox
	_, body = do(t, admin, "POST", e.srv.URL+"/api/projects", map[string]any{"path": t.TempDir(), "name": "shop"}, nil)
	pid := body["project"].(map[string]any)["id"].(string)
	e.st.Actions().Create(ctx, storage.Action{ProjectID: pid, Kind: "run_command", Target: "go test ./...", ProposedBy: "Claude Code CLI (admin@x.io)"})
	resp, body = do(t, admin, "GET", e.srv.URL+"/api/actions/pending", nil, nil)
	if resp.StatusCode != 200 || len(body["actions"].([]any)) != 1 {
		t.Fatalf("pending = %d %v", resp.StatusCode, body)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
