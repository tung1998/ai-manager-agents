package mcpgateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/mcpgateway"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// modernMCP speaks only MCP 2026-07-28: no initialize, no session; every
// request carries its version in _meta and mirrors method and name in
// headers. It records the methods and the Mcp-Param-Region header it got.
func modernMCP(t *testing.T, methods *[]string, region *string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string                     `json:"name"`
				Meta map[string]json.RawMessage `json:"_meta"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		mu.Lock()
		*methods = append(*methods, m.Method)
		if region != nil && r.Header.Get("Mcp-Param-Region") != "" {
			*region = r.Header.Get("Mcp-Param-Region")
		}
		mu.Unlock()
		reply := func(status int, v map[string]any) {
			v["jsonrpc"], v["id"] = "2.0", m.ID
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(v)
		}
		if string(m.Params.Meta["io.modelcontextprotocol/protocolVersion"]) != `"2026-07-28"` || r.Header.Get("Mcp-Protocol-Version") != "2026-07-28" {
			http.Error(w, "missing protocol version", http.StatusBadRequest) // as a legacy client sees it
			return
		}
		if r.Header.Get("Mcp-Method") != m.Method || (m.Method == "tools/call" && r.Header.Get("Mcp-Name") != m.Params.Name) {
			reply(400, map[string]any{"error": map[string]any{"code": -32020, "message": "Header mismatch"}})
			return
		}
		var result map[string]any
		switch m.Method {
		case "server/discover":
			result = map[string]any{"supportedVersions": []string{"2026-07-28"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "weather", "annotations": map[string]any{"readOnlyHint": true}}}, "ttlMs": 0, "cacheScope": "private"}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "sunny " + m.Params.Name}}}
		default:
			reply(404, map[string]any{"error": map[string]any{"code": -32601, "message": "Method not found"}})
			return
		}
		result["resultType"] = "complete"
		reply(200, map[string]any{"result": result})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Office checks a modern server statelessly: server/discover, then
// tools/list, never initialize.
func TestCheckModern(t *testing.T) {
	var methods []string
	srv := modernMCP(t, &methods, nil)
	tools, err := mcpgateway.Check(context.Background(), nil, srv.URL, nil)
	if err != nil || len(tools) != 1 || tools[0].Name != "weather" {
		t.Fatalf("tools = %+v %v", tools, err)
	}
	if strings.Join(methods, ",") != "server/discover,tools/list" {
		t.Fatalf("methods = %v", methods)
	}
}

// A legacy server that answers server/discover with an HTTP error gets the
// initialize handshake after it.
func TestCheckLegacyFallback(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if r.Method == http.MethodDelete { // the session's end
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		methods = append(methods, m.Method)
		if m.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != "s1" {
			http.Error(w, "Bad Request: No valid session ID provided", http.StatusBadRequest)
			return
		}
		if len(m.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result := map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}}
		if m.Method == "tools/list" {
			result = map[string]any{"tools": []any{map[string]any{"name": "old"}}}
		}
		w.Header().Set("Mcp-Session-Id", "s1")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
	}))
	t.Cleanup(srv.Close)
	tools, err := mcpgateway.Check(context.Background(), nil, srv.URL, nil)
	if err != nil || len(tools) != 1 || tools[0].Name != "old" {
		t.Fatalf("tools = %+v %v", tools, err)
	}
	if strings.Join(methods, ",") != "server/discover,initialize,notifications/initialized,tools/list" {
		t.Fatalf("methods = %v", methods)
	}
}

// Through the gateway a modern client's mirrored headers reach the server:
// Mcp-Method, Mcp-Name and the Mcp-Param-* of marked tool arguments, but only
// when they match the body.
func TestGatewayPassesModernHeaders(t *testing.T) {
	st, box := openStore(t)
	ctx := context.Background()
	var (
		methods []string
		region  string
	)
	srv := modernMCP(t, &methods, &region)
	ro := true
	m, _ := st.MCPServers().Create(ctx, storage.MCPServer{Name: "wx", URL: srv.URL, Enabled: true})
	if err := st.MCPServers().SetCheck(ctx, m.ID, "ok", "", []storage.MCPTool{{Name: "weather", ReadOnly: &ro}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	gw := &mcpgateway.Gateway{Store: st, Box: box, Identify: writerToken}
	mux := http.NewServeMux()
	mux.Handle("/mcp/s/{name}", gw)
	office := httptest.NewServer(mux)
	t.Cleanup(office.Close)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"weather","arguments":{"region":"us-west1"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	req, _ := http.NewRequest("POST", office.URL+"/mcp/s/wx", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer run-token")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", "weather")
	req.Header.Set("Mcp-Param-Region", "us-west1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(raw), "sunny weather") || region != "us-west1" {
		t.Fatalf("call = %d %s (region %q)", resp.StatusCode, raw, region)
	}

	// the policy read the body: an Mcp-Name naming another tool never reaches the server
	n := len(methods)
	req, _ = http.NewRequest("POST", office.URL+"/mcp/s/wx", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer run-token")
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", "delete_all")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != 400 || !strings.Contains(string(raw), "-32020") || len(methods) != n {
		t.Fatalf("mismatched Mcp-Name = %d %s (server saw %v)", resp.StatusCode, raw, methods[n:])
	}
}

// Office opens a stdio server with initialize, so the gateway answers a
// modern request for it as a legacy server does: 400 without a modern
// error, for the client to fall back to initialize.
func TestStdioRefusesModern(t *testing.T) {
	bin := fakeStdio(t)
	st, box := openStore(t)
	ctx := context.Background()
	if _, err := st.MCPServers().Create(ctx, storage.MCPServer{Name: "local", Kind: "stdio", Command: bin, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	gw := &mcpgateway.Gateway{Store: st, Box: box, Identify: writerToken}
	t.Cleanup(gw.Close)
	mux := http.NewServeMux()
	mux.Handle("/mcp/s/{name}", gw)
	office := httptest.NewServer(mux)
	t.Cleanup(office.Close)
	c := rpcClient{t, office.URL + "/mcp/s/local"}
	code, body := c.post(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`)
	if code != 400 || strings.Contains(body, "-320") {
		t.Fatalf("modern request = %d %s", code, body)
	}
	if code, body := c.post(`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}`); code != 200 || !strings.Contains(body, "fakestdio") {
		t.Fatalf("initialize after = %d %s", code, body)
	}
}
