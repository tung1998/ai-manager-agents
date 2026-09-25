// Package mcpserver serves the office tools to Claude Code over MCP
// (streamable HTTP, JSON responses, stateless). Each agent run gets its own
// bearer token bound to one project and revoked when the run ends, so an
// agent can only read the project it works on.
package mcpserver

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"bitbucket.org/senprints/agent-office/internal/officetools"
)

// ServerName is the MCP server name agents see (tools appear as mcp__office__*).
const ServerName = "office"

type grant struct {
	projectID string
	expires   time.Time
}

// Server is the MCP endpoint.
type Server struct {
	tools   *officetools.Toolbox
	version string

	mu     sync.Mutex
	grants map[string]grant
}

// New builds a Server.
func New(tools *officetools.Toolbox, version string) *Server {
	return &Server{tools: tools, version: version, grants: map[string]grant{}}
}

// Grant issues a token for projectID valid for ttl; call revoke when done.
func (s *Server) Grant(projectID string, ttl time.Duration) (token string, revoke func()) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	token = hex.EncodeToString(b)
	s.mu.Lock()
	now := time.Now()
	for k, g := range s.grants { // drop expired grants
		if now.After(g.expires) {
			delete(s.grants, k)
		}
	}
	s.grants[token] = grant{projectID: projectID, expires: now.Add(ttl)}
	s.mu.Unlock()
	return token, func() {
		s.mu.Lock()
		delete(s.grants, token)
		s.mu.Unlock()
	}
}

func (s *Server) project(r *http.Request) (string, bool) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.grants[tok]
	if !ok || time.Now().After(g.expires) {
		return "", false
	}
	return g.projectID, true
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ServeHTTP handles POST (JSON-RPC message or batch). GET (server stream) is
// not offered; DELETE (end session) is accepted and ignored.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	projectID, ok := s.project(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodPost:
	case http.MethodDelete:
		w.WriteHeader(http.StatusOK)
		return
	default:
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var (
		reqs  []request
		batch bool
	)
	if t := strings.TrimSpace(string(body)); strings.HasPrefix(t, "[") {
		batch = true
		err = json.Unmarshal(body, &reqs)
	} else {
		var one request
		err = json.Unmarshal(body, &one)
		reqs = []request{one}
	}
	if err != nil {
		writeJSON(w, response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}})
		return
	}
	var out []response
	for _, q := range reqs {
		if len(q.ID) == 0 { // notification: no reply
			continue
		}
		out = append(out, s.handle(r, projectID, q))
	}
	if len(out) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if batch {
		writeJSON(w, out)
		return
	}
	writeJSON(w, out[0])
}

func (s *Server) handle(r *http.Request, projectID string, q request) response {
	res := response{JSONRPC: "2.0", ID: q.ID}
	switch q.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(q.Params, &p)
		version := p.ProtocolVersion
		if version == "" {
			version = "2025-06-18"
		}
		res.Result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "agent-office", "version": s.version},
			"instructions":    "Thông tin vận hành của project (tiến trình build/dev/test, docker compose, giám sát). Chỉ đọc.",
		}
	case "ping":
		res.Result = map[string]any{}
	case "tools/list":
		list := []map[string]any{}
		for _, t := range s.tools.Tools() {
			list = append(list, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.Schema,
				"annotations": map[string]any{"readOnlyHint": true}})
		}
		res.Result = map[string]any{"tools": list}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(q.Params, &p); err != nil || !s.tools.Has(p.Name) {
			res.Error = &rpcError{Code: -32602, Message: "unknown tool"}
			return res
		}
		text, isErr := s.tools.Call(r.Context(), projectID, p.Name, p.Arguments)
		res.Result = map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}
	default:
		res.Error = &rpcError{Code: -32601, Message: "method not found: " + q.Method}
	}
	return res
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
