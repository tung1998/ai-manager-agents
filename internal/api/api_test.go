package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/api"
	"bitbucket.org/senprints/agent-office/internal/auth"
	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/orgmodel"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	officesetup "bitbucket.org/senprints/agent-office/internal/setup"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/transfer"
)

type env struct {
	srv  *httptest.Server
	auth *auth.Service
}

func setup(t *testing.T) *env { return setupWith(t, nil) }

func setupWith(t *testing.T, proxies []netip.Prefix) *env {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "office.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(st, auth.Options{Hasher: auth.FastHasherForTests()})
	box, err := secrets.Load(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	org := orgmodel.NewService(st)
	if _, err := org.SeedBuiltins(context.Background()); err != nil {
		t.Fatal(err)
	}
	provs := provider.NewService(st, box, llm.Options{})
	h := api.New(api.Config{Store: st, Auth: svc, AllowedOrigins: []string{"http://localhost:3000"}, TrustedProxies: proxies,
		Providers: provs, Org: org, Setup: officesetup.New(st, provs, org), Transfer: transfer.New(st, provs, org)})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	ctx := context.Background()
	if _, err := svc.CreateUser(ctx, auth.NewUser{Email: "admin@x.io", Name: "Admin", Role: storage.RoleAdmin, Password: "admin-password"}, "system"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(ctx, auth.NewUser{Email: "member@x.io", Role: storage.RoleMember, Password: "member-password"}, "system"); err != nil {
		t.Fatal(err)
	}
	return &env{srv: srv, auth: svc}
}

func (e *env) client(t *testing.T) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func do(t *testing.T, c *http.Client, method, url string, body any, hdr map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	var r *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, url, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func login(t *testing.T, e *env, c *http.Client, email, pw string) {
	t.Helper()
	resp, body := do(t, c, "POST", e.srv.URL+"/api/auth/login", map[string]string{"email": email, "password": pw}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("login %s = %d %v", email, resp.StatusCode, body)
	}
}

func TestHealthz(t *testing.T) {
	e := setup(t)
	for _, p := range []string{"/healthz", "/api/health"} {
		resp, _ := do(t, e.client(t), "GET", e.srv.URL+p, nil, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("%s = %d", p, resp.StatusCode)
		}
	}
}

func TestLoginFlow(t *testing.T) {
	e := setup(t)
	c := e.client(t)

	resp, _ := do(t, c, "GET", e.srv.URL+"/api/auth/me", nil, nil)
	if resp.StatusCode != 401 {
		t.Fatalf("me before login = %d", resp.StatusCode)
	}
	resp, body := do(t, c, "POST", e.srv.URL+"/api/auth/login", map[string]string{"email": "admin@x.io", "password": "nope-nope-nope"}, nil)
	if resp.StatusCode != 401 || body["error"] == nil {
		t.Fatalf("bad login = %d %v", resp.StatusCode, body)
	}
	resp, body = do(t, c, "POST", e.srv.URL+"/api/auth/login", map[string]string{"email": "admin@x.io", "password": "admin-password"}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("login = %d %v", resp.StatusCode, body)
	}
	var cookie *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == api.SessionCookie {
			cookie = ck
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("session cookie = %+v", cookie)
	}
	user := body["user"].(map[string]any)
	if user["email"] != "admin@x.io" || user["role"] != "admin" || user["password_hash"] != nil {
		t.Fatalf("user payload = %v", user)
	}
	resp, body = do(t, c, "GET", e.srv.URL+"/api/auth/me", nil, nil)
	if resp.StatusCode != 200 || body["user"].(map[string]any)["email"] != "admin@x.io" {
		t.Fatalf("me = %d %v", resp.StatusCode, body)
	}
	resp, _ = do(t, c, "POST", e.srv.URL+"/api/auth/logout", map[string]string{}, nil)
	if resp.StatusCode != 204 {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
	resp, _ = do(t, c, "GET", e.srv.URL+"/api/auth/me", nil, nil)
	if resp.StatusCode != 401 {
		t.Fatalf("me after logout = %d", resp.StatusCode)
	}
}

func TestCSRFGuards(t *testing.T) {
	e := setup(t)
	c := e.client(t)
	// form-encoded POST (what a cross-site <form> can send) is rejected
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/auth/login", bytes.NewBufferString("email=admin@x.io&password=admin-password"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("form post = %d", resp.StatusCode)
	}
	// foreign Origin is rejected
	resp, _ = do(t, c, "POST", e.srv.URL+"/api/auth/login", map[string]string{"email": "admin@x.io", "password": "admin-password"}, map[string]string{"Origin": "https://evil.example"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin = %d", resp.StatusCode)
	}
	// allowed Origin passes
	resp, _ = do(t, c, "POST", e.srv.URL+"/api/auth/login", map[string]string{"email": "admin@x.io", "password": "admin-password"}, map[string]string{"Origin": "http://localhost:3000"})
	if resp.StatusCode != 200 {
		t.Fatalf("allowed origin = %d", resp.StatusCode)
	}
}

func TestAdminUserManagement(t *testing.T) {
	e := setup(t)
	member := e.client(t)
	login(t, e, member, "member@x.io", "member-password")
	resp, _ := do(t, member, "GET", e.srv.URL+"/api/users", nil, nil)
	if resp.StatusCode != 403 {
		t.Fatalf("member list users = %d", resp.StatusCode)
	}

	admin := e.client(t)
	login(t, e, admin, "admin@x.io", "admin-password")
	resp, body := do(t, admin, "GET", e.srv.URL+"/api/users", nil, nil)
	if resp.StatusCode != 200 || len(body["users"].([]any)) != 2 {
		t.Fatalf("admin list users = %d %v", resp.StatusCode, body)
	}
	resp, body = do(t, admin, "POST", e.srv.URL+"/api/users", map[string]string{"email": "new@x.io", "name": "New", "role": "member", "password": "new-user-password"}, nil)
	if resp.StatusCode != 201 {
		t.Fatalf("create user = %d %v", resp.StatusCode, body)
	}
	newID := body["user"].(map[string]any)["id"].(string)
	resp, body = do(t, admin, "POST", e.srv.URL+"/api/users", map[string]string{"email": "new@x.io", "role": "member", "password": "new-user-password"}, nil)
	if resp.StatusCode != 409 {
		t.Fatalf("duplicate user = %d %v", resp.StatusCode, body)
	}
	resp, body = do(t, admin, "POST", e.srv.URL+"/api/users", map[string]string{"email": "weak@x.io", "role": "member", "password": "short"}, nil)
	if resp.StatusCode != 400 {
		t.Fatalf("weak password = %d %v", resp.StatusCode, body)
	}

	nc := e.client(t)
	login(t, e, nc, "new@x.io", "new-user-password")
	resp, _ = do(t, admin, "PATCH", e.srv.URL+"/api/users/"+newID, map[string]any{"disabled": true}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("disable = %d", resp.StatusCode)
	}
	resp, _ = do(t, nc, "GET", e.srv.URL+"/api/auth/me", nil, nil)
	if resp.StatusCode != 401 {
		t.Fatalf("disabled user session = %d", resp.StatusCode)
	}
	// admin cannot disable themself (would lock out the office)
	resp, body = do(t, admin, "GET", e.srv.URL+"/api/auth/me", nil, nil)
	selfID := body["user"].(map[string]any)["id"].(string)
	resp, _ = do(t, admin, "PATCH", e.srv.URL+"/api/users/"+selfID, map[string]any{"disabled": true}, nil)
	if resp.StatusCode != 400 {
		t.Fatalf("self disable = %d", resp.StatusCode)
	}
	resp, body = do(t, admin, "GET", e.srv.URL+"/api/audit?limit=50", nil, nil)
	if resp.StatusCode != 200 || len(body["entries"].([]any)) == 0 {
		t.Fatalf("audit = %d %v", resp.StatusCode, body)
	}
}

func TestChangeOwnPassword(t *testing.T) {
	e := setup(t)
	c := e.client(t)
	login(t, e, c, "member@x.io", "member-password")
	resp, _ := do(t, c, "POST", e.srv.URL+"/api/auth/password", map[string]string{"old_password": "wrong-wrong-1", "new_password": "brand-new-pass"}, nil)
	if resp.StatusCode != 400 {
		t.Fatalf("wrong old = %d", resp.StatusCode)
	}
	resp, _ = do(t, c, "POST", e.srv.URL+"/api/auth/password", map[string]string{"old_password": "member-password", "new_password": "brand-new-pass"}, nil)
	if resp.StatusCode != 204 {
		t.Fatalf("change = %d", resp.StatusCode)
	}
	resp, _ = do(t, c, "GET", e.srv.URL+"/api/auth/me", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("session after change = %d", resp.StatusCode)
	}
}

func TestForwardedHostFromTrustedProxy(t *testing.T) {
	body := map[string]string{"email": "admin@x.io", "password": "admin-password"}
	hdr := map[string]string{"Origin": "https://office.example.com", "X-Forwarded-Host": "office.example.com"}

	untrusted := setup(t)
	if resp, _ := do(t, untrusted.client(t), "POST", untrusted.srv.URL+"/api/auth/login", body, hdr); resp.StatusCode != 403 {
		t.Fatalf("untrusted peer forwarded host = %d, want 403", resp.StatusCode)
	}
	trusted := setupWith(t, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
	if resp, _ := do(t, trusted.client(t), "POST", trusted.srv.URL+"/api/auth/login", body, hdr); resp.StatusCode != 200 {
		t.Fatalf("trusted peer forwarded host = %d, want 200", resp.StatusCode)
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
