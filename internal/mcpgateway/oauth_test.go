package mcpgateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// fakeAuth is an authorization server (RFC 8414 + 7591 + PKCE) and an MCP
// server that only takes the token it issued last.
type fakeAuth struct {
	t           *testing.T
	as, mcp     *httptest.Server
	mu          sync.Mutex
	challenges  map[string]string // code → its code_challenge
	redirects   []string          // registered
	issued      int
	current     string // the only access token the MCP server takes
	refreshes   int
	refreshFail bool
	firstTTL    int // expires_in of the first token
	seen        []string
	noDCR       bool
}

func newFakeAuth(t *testing.T) *fakeAuth {
	f := &fakeAuth{t: t, challenges: map[string]string{}, firstTTL: 3600}
	f.as = httptest.NewServer(http.HandlerFunc(f.serveAS))
	f.mcp = httptest.NewServer(http.HandlerFunc(f.serveMCP))
	t.Cleanup(f.as.Close)
	t.Cleanup(f.mcp.Close)
	return f
}

func (f *fakeAuth) mcpURL() string { return f.mcp.URL + "/mcp" }

func (f *fakeAuth) serveAS(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/.well-known/oauth-authorization-server":
		meta := map[string]any{"issuer": f.as.URL, "authorization_endpoint": f.as.URL + "/authorize", "token_endpoint": f.as.URL + "/token",
			"code_challenge_methods_supported": []string{"S256"}}
		if !f.noDCR {
			meta["registration_endpoint"] = f.as.URL + "/register"
		}
		json.NewEncoder(w).Encode(meta)
	case "/register":
		var in struct {
			RedirectURIs []string `json:"redirect_uris"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if len(in.RedirectURIs) == 1 && strings.HasPrefix(in.RedirectURIs[0], "http://office.lan") {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]any{"error": "invalid_redirect_uri", "error_description": "http only on localhost"})
			return
		}
		f.redirects = append(f.redirects, in.RedirectURIs...)
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"client_id": "cid", "token_endpoint_auth_method": "none"})
	case "/token":
		r.ParseForm()
		if r.Form.Get("resource") != f.mcpURL() || r.Form.Get("client_id") != "cid" {
			f.t.Errorf("token request = %v", r.Form)
		}
		ttl := 3600
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			ch, ok := f.challenges[r.Form.Get("code")]
			delete(f.challenges, r.Form.Get("code"))
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != ch || !strings.HasSuffix(r.Form.Get("redirect_uri"), CallbackPath) {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
				return
			}
			ttl = f.firstTTL
		case "refresh_token":
			f.refreshes++
			if f.refreshFail || r.Form.Get("refresh_token") != "rt" {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
				return
			}
		}
		f.issued++
		f.current = fmt.Sprintf("at-%d", f.issued)
		json.NewEncoder(w).Encode(map[string]any{"access_token": f.current, "refresh_token": "rt", "expires_in": ttl, "token_type": "Bearer"})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAuth) serveMCP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	cur := f.current
	f.seen = append(f.seen, r.Header.Get("Authorization"))
	f.mu.Unlock()
	if r.URL.Path == "/.well-known/oauth-protected-resource/mcp" {
		json.NewEncoder(w).Encode(map[string]any{"resource": f.mcpURL(), "authorization_servers": []string{f.as.URL}})
		return
	}
	if cur == "" || r.Header.Get("Authorization") != "Bearer "+cur {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+f.mcp.URL+`/.well-known/oauth-protected-resource/mcp"`)
		http.Error(w, "login", http.StatusUnauthorized)
		return
	}
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	json.NewDecoder(r.Body).Decode(&m)
	if len(m.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any = map[string]any{}
	switch m.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{}}
	case "tools/list":
		result = map[string]any{"tools": []any{map[string]any{"name": "search"}}}
	case "tools/call":
		result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "found"}}}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
}

// authorize plays the person logging in: the server keeps the challenge
// under a new code and sends them back.
func (f *fakeAuth) authorize(t *testing.T, authURL string) (state, code string) {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil || !strings.HasPrefix(authURL, f.as.URL+"/authorize?") {
		t.Fatalf("auth URL = %s", authURL)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 || q.Get("resource") != f.mcpURL() ||
		q.Get("client_id") != "cid" || q.Get("response_type") != "code" || q.Get("state") == "" {
		t.Fatalf("auth query = %v", q)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	code = fmt.Sprintf("code-%d", len(f.challenges)+f.issued)
	f.challenges[code] = q.Get("code_challenge")
	return q.Get("state"), code
}

func (f *fakeAuth) revoke() {
	f.mu.Lock()
	f.current = "revoked"
	f.mu.Unlock()
}

func setupOAuth(t *testing.T) (*fakeAuth, *Gateway, storage.Store, storage.MCPServer, *[]storage.MCPServer) {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "office.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	box, _ := secrets.Load(filepath.Join(t.TempDir(), "secret.key"))
	f := newFakeAuth(t)
	m, err := st.MCPServers().Create(context.Background(), storage.MCPServer{Name: "remote", URL: f.mcpURL(), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var heard []storage.MCPServer
	gw := &Gateway{Store: st, Box: box, Auth: func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer run-token" },
		OnStatus: func(m storage.MCPServer) { heard = append(heard, m) }}
	return f, gw, st, m, &heard
}

func TestPKCE(t *testing.T) {
	v, c := pkce()
	sum := sha256.Sum256([]byte(v))
	if len(v) != 43 || c != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Fatalf("verifier %q challenge %q", v, c)
	}
	if v2, _ := pkce(); v2 == v {
		t.Fatal("verifier repeats")
	}
}

func TestDiscover(t *testing.T) {
	f := newFakeAuth(t)
	// named in WWW-Authenticate
	o, err := Discover(context.Background(), nil, f.mcpURL()+"?key=sk-1", `Bearer realm="x", resource_metadata="`+f.mcp.URL+`/.well-known/oauth-protected-resource/mcp", scope="read write"`)
	if err != nil || o.AuthEndpoint != f.as.URL+"/authorize" || o.TokenEndpoint != f.as.URL+"/token" || o.RegistrationEndpoint != f.as.URL+"/register" ||
		o.Resource != f.mcpURL() || o.Scope != "read write" || o.Issuer != f.as.URL {
		t.Fatalf("discover = %+v %v", o, err)
	}
	// not named: the well-known address under the server
	if o, err = Discover(context.Background(), nil, f.mcpURL(), "Bearer"); err != nil || o.TokenEndpoint != f.as.URL+"/token" {
		t.Fatalf("discover well-known = %+v %v", o, err)
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	defer dead.Close()
	if _, err := Discover(context.Background(), nil, dead.URL+"/mcp", "Bearer"); err == nil {
		t.Fatal("found an authorization server where there is none")
	}
}

func TestOAuthLogin(t *testing.T) {
	f, gw, st, m, heard := setupOAuth(t)
	ctx := context.Background()

	// a check without a login: needs_login, not a plain error
	m, err := gw.CheckServer(ctx, m)
	if err != nil || m.LastCheckStatus != "needs_login" {
		t.Fatalf("check before login = %+v %v", m, err)
	}
	if o, _ := OpenOAuth(gw.Box, m.OAuthEnc); !o.Discovered() || o.LoggedIn() {
		t.Fatalf("oauth after check = %+v", o)
	}

	// DCR refusing the callback address: a hint to open office on localhost
	if _, err := gw.StartLogin(ctx, m, "u1", "http://office.lan:8787"); !errors.Is(err, ErrRedirectRejected) || !strings.Contains(err.Error(), "http://localhost:8787") {
		t.Fatalf("rejected redirect = %v", err)
	}

	authURL, err := gw.StartLogin(ctx, m, "u1", "http://localhost:2704")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.redirects) != 1 || f.redirects[0] != "http://localhost:2704"+CallbackPath {
		t.Fatalf("registered = %v", f.redirects)
	}
	state, code := f.authorize(t, authURL)

	// another person, a wrong state: refused; a state works once only
	if _, err := gw.FinishLogin(ctx, "u2", state, code, ""); !errors.Is(err, ErrBadState) {
		t.Fatalf("other user = %v", err)
	}
	if _, err := gw.FinishLogin(ctx, "u1", state, code, ""); !errors.Is(err, ErrBadState) {
		t.Fatalf("used state = %v", err)
	}
	if _, err := gw.FinishLogin(ctx, "u1", "made-up", code, ""); !errors.Is(err, ErrBadState) {
		t.Fatalf("unknown state = %v", err)
	}
	// expired
	authURL, _ = gw.StartLogin(ctx, m, "u1", "http://localhost:2704")
	state, code = f.authorize(t, authURL)
	gw.logins.mu.Lock()
	p := gw.logins.pending[state]
	p.exp = time.Now().Add(-time.Second)
	gw.logins.pending[state] = p
	gw.logins.mu.Unlock()
	if _, err := gw.FinishLogin(ctx, "u1", state, code, ""); !errors.Is(err, ErrBadState) {
		t.Fatalf("expired state = %v", err)
	}
	if len(f.redirects) != 1 {
		t.Fatalf("registered again for the same address: %v", f.redirects)
	}

	// the real login: the first token expires within a minute
	f.firstTTL = 30
	authURL, _ = gw.StartLogin(ctx, m, "u1", "http://localhost:2704")
	state, code = f.authorize(t, authURL)
	m, err = gw.FinishLogin(ctx, "u1", state, code, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.OAuthEnc, "at-1") || strings.Contains(m.OAuthEnc, `"rt"`) {
		t.Fatal("tokens stored in clear")
	}

	// close to expiry: refreshed before use
	m, _ = gw.CheckServer(ctx, m)
	if m.LastCheckStatus != "ok" || len(m.LastTools) != 1 || f.refreshes != 1 || f.current != "at-2" {
		t.Fatalf("check after login = %+v refreshes=%d current=%s", m, f.refreshes, f.current)
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp/s/{name}", gw)
	office := httptest.NewServer(mux)
	t.Cleanup(office.Close)
	call := func() (int, string) {
		req, _ := http.NewRequest("POST", office.URL+"/mcp/s/remote", bytes.NewBufferString(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search"}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer run-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}
	if code, body := call(); code != 200 || !strings.Contains(body, "found") || f.refreshes != 1 {
		t.Fatalf("call = %d %s refreshes=%d", code, body, f.refreshes)
	}
	for _, a := range f.seen {
		if a == "Bearer run-token" {
			t.Fatal("the run's token reached the server")
		}
	}

	// the server drops the token: 401 → refresh → once more
	f.revoke()
	if code, body := call(); code != 200 || !strings.Contains(body, "found") || f.refreshes != 2 {
		t.Fatalf("after revoke = %d %s refreshes=%d", code, body, f.refreshes)
	}

	// the refresh is refused: needs_login, open pages hear it
	f.refreshFail = true
	f.revoke()
	status, body := call()
	if status != 502 || !strings.Contains(body, "phiên hết hạn") || f.refreshes != 3 {
		t.Fatalf("refused refresh = %d %s refreshes=%d", status, body, f.refreshes)
	}
	got, _ := st.MCPServers().Get(ctx, m.ID)
	o, _ := OpenOAuth(gw.Box, got.OAuthEnc)
	if got.LastCheckStatus != "needs_login" || !o.Expired || o.LoggedIn() || len(*heard) == 0 || (*heard)[len(*heard)-1].LastCheckStatus != "needs_login" {
		t.Fatalf("after refused refresh = %+v %+v heard=%d", got, o, len(*heard))
	}
	if code, body := call(); code != 502 || !strings.Contains(body, "Kết nối lại") || f.refreshes != 3 {
		t.Fatalf("while logged out = %d %s refreshes=%d", code, body, f.refreshes)
	}
	if got, _ = gw.CheckServer(ctx, got); got.LastCheckStatus != "needs_login" || !strings.Contains(got.LastCheckError, "Kết nối lại") {
		t.Fatalf("check while expired = %+v", got)
	}

	// log in again, then log out: tokens gone
	f.refreshFail, f.firstTTL = false, 3600
	authURL, _ = gw.StartLogin(ctx, got, "u1", "http://localhost:2704")
	state, code2 := f.authorize(t, authURL)
	if m, err = gw.FinishLogin(ctx, "u1", state, code2, ""); err != nil {
		t.Fatal(err)
	}
	if m, err = gw.Logout(ctx, m); err != nil || m.LastCheckStatus != "needs_login" {
		t.Fatalf("logout = %+v %v", m, err)
	}
	if o, _ := OpenOAuth(gw.Box, m.OAuthEnc); o.LoggedIn() || !o.Discovered() || o.ClientID != "cid" {
		t.Fatalf("after logout = %+v", o)
	}
}

func TestOAuthAuthError(t *testing.T) {
	f, gw, _, m, _ := setupOAuth(t)
	ctx := context.Background()
	m, _ = gw.CheckServer(ctx, m)
	authURL, _ := gw.StartLogin(ctx, m, "u1", "http://192.168.1.5:2704")
	state, _ := f.authorize(t, authURL)
	_, err := gw.FinishLogin(ctx, "u1", state, "", "invalid_request")
	if err == nil || !strings.Contains(err.Error(), "http://localhost:2704") {
		t.Fatalf("auth error = %v", err)
	}
}

func TestOAuthNoDCR(t *testing.T) {
	f, gw, st, m, _ := setupOAuth(t)
	f.noDCR = true
	ctx := context.Background()
	m, _ = gw.CheckServer(ctx, m)
	if _, err := gw.StartLogin(ctx, m, "u1", "http://localhost:2704"); err == nil || !strings.Contains(err.Error(), "client_id") {
		t.Fatalf("no DCR = %v", err)
	}
	// a typed client is used as it is
	m, _ = st.MCPServers().Get(ctx, m.ID)
	o, _ := OpenOAuth(gw.Box, m.OAuthEnc)
	o.ClientID, o.ClientManual = "cid", true
	enc, _ := SealOAuth(gw.Box, o)
	st.MCPServers().SetOAuth(ctx, m.ID, enc)
	m.OAuthEnc = enc
	authURL, err := gw.StartLogin(ctx, m, "u1", "http://localhost:2704")
	if err != nil {
		t.Fatal(err)
	}
	state, code := f.authorize(t, authURL)
	if _, err := gw.FinishLogin(ctx, "u1", state, code, ""); err != nil {
		t.Fatal(err)
	}
}
