package api_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// ADR-093: a project's .mcp.json server moves into office (its secret into
// the encrypted store, out of the file with a copy in trash), is given to
// some agents, logs its calls, and goes back on request.
func TestMCPImportAPI(t *testing.T) {
	e := setup(t)
	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	ctx := context.Background()
	up := upstreamMCP(t)
	dir := t.TempDir()
	if _, err := e.st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: dir}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OFFICE_TEST_MCP_TOKEN", strings.TrimPrefix(mcpSecret, "Bearer "))
	mcpJSON := `{"mcpServers":{"Docs_Server":{"type":"http","url":"` + up.URL + `","headers":{"Authorization":"Bearer ${OFFICE_TEST_MCP_TOKEN}"}},` +
		`"old":{"type":"sse","url":"https://x.example/sse"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcpJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, body := do(t, admin, "GET", e.srv.URL+"/api/mcp/import", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("candidates = %d %v", resp.StatusCode, body)
	}
	var docs map[string]any
	for _, c := range body["candidates"].([]any) {
		c := c.(map[string]any)
		switch c["name"] {
		case "Docs_Server":
			docs = c
		case "old":
			if c["problem"] != "sse" {
				t.Fatalf("sse candidate = %v", c)
			}
		}
	}
	if docs == nil || docs["suggested"] != "docs-server" || docs["movable"] != true || docs["target"] != up.URL {
		t.Fatalf("docs candidate = %v", docs)
	}
	resp, body = do(t, admin, "POST", e.srv.URL+"/api/mcp/import", map[string]any{"take_out": true,
		"items": []any{map[string]any{"ref": docs["ref"], "name": "docs"}}}, nil)
	res := body["results"].([]any)[0].(map[string]any)
	if resp.StatusCode != 200 || res["ok"] != true || res["taken_out"] != true {
		t.Fatalf("import = %d %v", resp.StatusCode, body)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "very-secret") {
		t.Fatalf("import answer carries the secret: %s", raw)
	}
	left, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if strings.Contains(string(left), "Docs_Server") || !strings.Contains(string(left), "old") {
		t.Fatalf(".mcp.json after = %s", left)
	}
	srv := res["server"].(map[string]any)
	id := srv["id"].(string)
	m, _ := e.st.MCPServers().Get(ctx, id)
	if m.Origin != "mcp.json" || m.URL != up.URL || !strings.Contains(m.OriginRef, "Docs_Server") || !strings.Contains(m.OriginRef, "trash") ||
		strings.Contains(m.OriginRef, "OFFICE_TEST_MCP_TOKEN") {
		t.Fatalf("stored = %+v", m)
	}
	if from := srv["moved_from"].(map[string]any); from["name"] != "Docs_Server" || from["backup"] != true {
		t.Fatalf("moved_from = %v", from)
	}
	// the ${VAR} was filled in: the stored secret works
	if resp, body = do(t, admin, "POST", e.srv.URL+"/api/mcp/servers/"+id+"/check", nil, nil); body["server"].(map[string]any)["last_check_status"] != "ok" {
		t.Fatalf("check after import = %v", body)
	}
	if resp, body = do(t, admin, "POST", e.srv.URL+"/api/mcp/import", map[string]any{"items": []any{map[string]any{"ref": docs["ref"], "name": "docs"}}}, nil); resp.StatusCode != 200 {
		t.Fatalf("import again = %d", resp.StatusCode)
	} else if r := body["results"].([]any)[0].(map[string]any); r["ok"] == true {
		t.Fatalf("a server no longer in the file imported again: %v", r)
	}

	// given to one agent, one tool trusted
	resp, body = do(t, e.srv.Client(), "GET", e.srv.URL+"/api/mcp/agents", nil, nil)
	if resp.StatusCode == 200 {
		t.Fatal("agents list without login")
	}
	resp, body = do(t, admin, "GET", e.srv.URL+"/api/mcp/agents", nil, nil)
	if resp.StatusCode != 200 || len(body["agents"].([]any)) == 0 {
		t.Fatalf("agents = %d %v", resp.StatusCode, body)
	}
	resp, body = do(t, admin, "PATCH", e.srv.URL+"/api/mcp/servers/"+id, map[string]any{"agents": []string{"agt_x", " agt_x ", ""}, "trusted_tools": []string{"resolve-library-id"}}, nil)
	if s := body["server"].(map[string]any); resp.StatusCode != 200 || len(s["agents"].([]any)) != 1 || len(s["trusted_tools"].([]any)) != 1 {
		t.Fatalf("assign = %d %v", resp.StatusCode, body)
	}

	// the call log
	_ = e.st.MCPCalls().Add(ctx, storage.MCPCall{ServerID: id, ServerName: "docs", Tool: "resolve-library-id", Caller: "Dev", CallerKind: "claude", Status: "ok"})
	resp, body = do(t, admin, "GET", e.srv.URL+"/api/mcp/servers/"+id+"/calls", nil, nil)
	if calls := body["calls"].([]any); resp.StatusCode != 200 || len(calls) != 1 || calls[0].(map[string]any)["caller"] != "Dev" {
		t.Fatalf("calls = %d %v", resp.StatusCode, body)
	}
	resp, body = do(t, admin, "GET", e.srv.URL+"/api/mcp/servers", nil, nil)
	if st := body["stats"].(map[string]any)[id].(map[string]any); st["calls"] != float64(1) {
		t.Fatalf("stats = %v", body["stats"])
	}

	// back to .mcp.json, and out of office
	resp, body = do(t, admin, "POST", e.srv.URL+"/api/mcp/servers/"+id+"/put-back", map[string]any{"delete": true}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("put back = %d %v", resp.StatusCode, body)
	}
	back, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	// as it was: the ${VAR}, never the secret office filled in
	if !strings.Contains(string(back), "Docs_Server") || !strings.Contains(string(back), "${OFFICE_TEST_MCP_TOKEN}") || strings.Contains(string(back), "very-secret") {
		t.Fatalf(".mcp.json after put back = %s", back)
	}
	if _, err := e.st.MCPServers().Get(ctx, id); err == nil {
		t.Fatal("still in office after put back with delete")
	}
}
