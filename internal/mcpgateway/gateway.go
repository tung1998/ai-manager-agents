// Package mcpgateway is the office's MCP gateway (ADR-091): each MCP server
// office manages has its own address /mcp/s/<name>, so its tools keep their
// names (mcp__<name>__*). Office checks the run's token, adds the server's
// secrets and forwards the JSON-RPC as it is; answers come back as JSON or
// an SSE stream.
package mcpgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// CallTimeout bounds one forwarded request (a tool call may take a while).
const CallTimeout = 10 * time.Minute

// Gateway forwards agent runs' MCP requests to the servers office manages.
type Gateway struct {
	Store storage.Store
	Box   Sealer
	// Auth accepts the bearer token of a run (or a person's own CLI, ADR-047).
	Auth   func(r *http.Request) bool
	Client *http.Client // nil: a default one
	Log    *slog.Logger
}

func (g *Gateway) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return http.DefaultClient
}

func (g *Gateway) log() *slog.Logger {
	if g.Log != nil {
		return g.Log
	}
	return slog.Default()
}

// Names lists the servers agent runs get (GĐ1: every enabled HTTP server
// of the machine, for every agent).
func (g *Gateway) Names(ctx context.Context) []string {
	list, err := g.Store.MCPServers().List(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range list {
		if m.Enabled && m.Kind == "http" && m.Scope == "machine" {
			out = append(out, m.Name)
		}
	}
	return out
}

// CheckServer checks m and saves the result.
func (g *Gateway) CheckServer(ctx context.Context, m storage.MCPServer) (storage.MCPServer, error) {
	var (
		tools []storage.MCPTool
		err   error
	)
	if m.Kind != "http" {
		err = errors.New("office chưa chạy được MCP stdio")
	} else {
		var headers map[string]string
		if headers, err = OpenMap(g.Box, m.HeadersEnc); err != nil {
			err = errors.New("không mở được bí mật đã lưu: nhập lại header")
		} else {
			tools, err = Check(ctx, g.client(), m.URL, headers)
		}
	}
	status, msg := "ok", ""
	if err != nil {
		status, msg, tools = "error", err.Error(), m.LastTools
	}
	now := time.Now().UTC()
	if serr := g.Store.MCPServers().SetCheck(context.WithoutCancel(ctx), m.ID, status, msg, tools, now); serr != nil {
		return m, serr
	}
	m.LastCheckAt, m.LastCheckStatus, m.LastCheckError, m.LastTools = &now, status, msg, tools
	g.log().Info("mcp gateway: check", "server", m.Name, "status", status, "tools", len(tools))
	return m, nil
}

// request headers passed on to the server (never the run's Authorization)
var passRequest = []string{"Accept", "Content-Type", "Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-Id"}

// response headers passed back
var passResponse = []string{"Content-Type", "Mcp-Session-Id", "Mcp-Protocol-Version"}

// ServeHTTP handles /mcp/s/{name}.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if g.Auth == nil || !g.Auth(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodPost, http.MethodGet, http.MethodDelete:
	default:
		w.Header().Set("Allow", "POST, GET, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.PathValue("name")
	m, err := g.Store.MCPServers().GetByName(r.Context(), name)
	if err != nil || !m.Enabled || m.Kind != "http" {
		http.Error(w, "office không có MCP "+name, http.StatusNotFound)
		return
	}
	headers, err := OpenMap(g.Box, m.HeadersEnc)
	if err != nil {
		g.log().Error("mcp gateway: open secrets", "server", name, "err", err)
		http.Error(w, "office không mở được bí mật của MCP "+name, http.StatusInternalServerError)
		return
	}
	var body []byte
	if r.Method == http.MethodPost {
		if body, err = io.ReadAll(io.LimitReader(r.Body, 8<<20)); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
	}
	rpc, tool := describe(body)

	ctx, cancel := r.Context(), context.CancelFunc(func() {})
	if r.Method != http.MethodGet { // GET is the server's own stream: open while the client listens
		ctx, cancel = context.WithTimeout(ctx, CallTimeout)
	}
	defer cancel()
	up, err := http.NewRequestWithContext(ctx, r.Method, m.URL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "URL của MCP "+name+" không hợp lệ", http.StatusBadGateway)
		return
	}
	for _, h := range passRequest {
		if v := r.Header.Get(h); v != "" {
			up.Header.Set(h, v)
		}
	}
	for k, v := range headers {
		up.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := g.client().Do(up)
	if err != nil {
		if r.Context().Err() == nil {
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err // the URL may carry a key
			}
			g.log().Warn("mcp gateway", "server", name, "rpc", rpc, "tool", tool, "err", err.Error(), "ms", time.Since(start).Milliseconds())
			http.Error(w, "office không gọi được MCP "+name+": "+err.Error(), http.StatusBadGateway)
		}
		return
	}
	defer resp.Body.Close()
	g.log().Info("mcp gateway", "server", name, "method", r.Method, "rpc", rpc, "tool", tool, "status", resp.StatusCode, "ms", time.Since(start).Milliseconds())
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// the server refused office's secret: not the run's token, so no
		// WWW-Authenticate (the CLI would start a login against office)
		http.Error(w, "MCP "+name+" từ chối token đã lưu trong office (HTTP "+strconv.Itoa(resp.StatusCode)+"): sửa trên dashboard", http.StatusBadGateway)
		return
	}
	for _, h := range passResponse {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt == "text/event-stream" {
		stream(w, resp.Body)
		return
	}
	_, _ = io.Copy(w, resp.Body)
}

// stream copies an SSE body, flushing as events arrive.
func stream(w http.ResponseWriter, body io.Reader) {
	rc := http.NewResponseController(w)
	buf := make([]byte, 32<<10)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			_ = rc.Flush()
		}
		if err != nil {
			return
		}
	}
}

// describe names a JSON-RPC request for the log: its method and, for a tool
// call, the tool (never the arguments).
func describe(body []byte) (rpc, tool string) {
	var one struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	t := bytes.TrimSpace(body)
	if len(t) == 0 {
		return "", ""
	}
	if t[0] == '[' {
		var batch []json.RawMessage
		if json.Unmarshal(t, &batch) != nil || len(batch) == 0 {
			return "batch", ""
		}
		_ = json.Unmarshal(batch[0], &one)
		return "batch:" + one.Method, one.Params.Name
	}
	_ = json.Unmarshal(t, &one)
	if one.Method != "tools/call" {
		one.Params.Name = ""
	}
	return one.Method, one.Params.Name
}
