package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/officetools"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func TestMCP(t *testing.T) {
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	ctx := context.Background()
	st.Migrate(ctx)
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "p", Path: t.TempDir()})
	other, _ := st.Repos().Create(ctx, storage.Repo{Name: "q", Path: t.TempDir()})
	st.Monitors().Create(ctx, storage.Monitor{ProjectID: p.ID, Name: "web", Type: "http", Target: "http://x", IntervalS: 60, Enabled: true, Status: "down", LastMessage: "HTTP 500"})
	st.Monitors().Create(ctx, storage.Monitor{ProjectID: other.ID, Name: "secret", Type: "http", Target: "http://y", IntervalS: 60, Enabled: true})

	s := New(officetools.New(st, nil, nil), "test")
	srv := httptest.NewServer(s)
	defer srv.Close()
	tok, revoke := s.Grant(officetools.Scope{ProjectID: p.ID}, time.Minute)

	call := func(token, body string) (int, map[string]any) {
		req, _ := http.NewRequest("POST", srv.URL, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if code, _ := call("", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); code != 401 {
		t.Fatalf("no token: %d", code)
	}
	_, init := call(tok, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
	if init["result"].(map[string]any)["protocolVersion"] != "2025-03-26" {
		t.Fatal(init)
	}
	if code, _ := call(tok, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); code != 202 {
		t.Fatalf("notification: %d", code)
	}
	_, list := call(tok, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if n := len(list["result"].(map[string]any)["tools"].([]any)); n != 9 { // + read_link, search_history
		t.Fatalf("tools: %d", n)
	}
	_, ov := call(tok, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ops_overview","arguments":{}}}`)
	text := ov["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "web") || !strings.Contains(text, "HTTP 500") || strings.Contains(text, "secret") {
		t.Fatalf("overview must show only this project: %s", text)
	}
	_, md := call(tok, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"monitor_detail","arguments":{"name":"secret"}}}`)
	if md["result"].(map[string]any)["isError"] != true {
		t.Fatal("other project's monitor must not be readable")
	}
	revoke()
	if code, _ := call(tok, `{"jsonrpc":"2.0","id":5,"method":"tools/list"}`); code != 401 {
		t.Fatalf("revoked: %d", code)
	}
}

// TestReadOnlyHint checks that readOnlyHint in the wire format mirrors each
// Tool's ReadOnly field. This toolbox has no actions/config/burn wired in, so
// tools/list here only returns the read-only subset (ops_overview and
// friends, plus the office tools since the scope is Office); the full set of
// tools, including the write ones, is covered by
// officetools.TestToolsReadOnly.

func TestReadOnlyHint(t *testing.T) {
	st, _ := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	defer st.Close()
	ctx := context.Background()
	st.Migrate(ctx)
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "p", Path: t.TempDir()})

	readOnly := map[string]bool{
		"projects": true, "jobs_query": true, "usage_summary": true, "handoff": true,
		"ops_overview": true, "process_logs": true, "container_logs": true, "monitor_detail": true,
		"git_status": true, "git_diff": true, "git_log": true,
		"describe": true, "list": true, "get": true,
		"search_history": true, "read_link": true, "burn_list": true,
	}

	tb := officetools.New(st, nil, nil)
	s := New(tb, "test")
	srv := httptest.NewServer(s)
	defer srv.Close()
	tok, revoke := s.Grant(officetools.Scope{ProjectID: p.ID, Level: perm.Propose, Office: true}, time.Minute)
	defer revoke()

	req, _ := http.NewRequest("POST", srv.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	tools := out["result"].(map[string]any)["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("no tools returned")
	}
	for _, raw := range tools {
		tl := raw.(map[string]any)
		name := tl["name"].(string)
		want := readOnly[name]
		got := tl["annotations"].(map[string]any)["readOnlyHint"].(bool)
		if got != want {
			t.Errorf("tool %q: readOnlyHint=%v, want %v", name, got, want)
		}
	}
}
