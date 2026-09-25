// Package api is the HTTP surface of the office server: dashboard REST API,
// health check, and (later) SSE and webhook ingress.
package api

import (
	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/monitor"
	"bitbucket.org/senprints/agent-office/internal/ops"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/auth"
	"bitbucket.org/senprints/agent-office/internal/automation"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/clitools"
	"bitbucket.org/senprints/agent-office/internal/orgmodel"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/setup"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/tasks"
	"bitbucket.org/senprints/agent-office/internal/transfer"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// SessionCookie is the name of the login cookie.
const SessionCookie = "office_session"

const maxBody = 64 << 10

// Config wires the API.
type Config struct {
	Store          storage.Store
	Auth           *auth.Service
	AllowedOrigins []string       // extra origins allowed on state-changing requests (the dashboard)
	SecureCookies  bool           // set Secure on the cookie; required when served over HTTPS
	TrustedProxies []netip.Prefix // peers whose X-Forwarded-* headers are honoured (the dashboard proxy)
	Logger         *slog.Logger
	Version        string

	Providers  *provider.Service // nil disables the provider/model/repo routes (auth-only tests)
	Org        *orgmodel.Service
	Setup      *setup.Assistant
	Transfer   *transfer.Service
	Usage      *usage.Service
	CLITools   *clitools.Manager // nil: installing/signing in CLIs from the dashboard is off
	Chat       *chat.Engine
	Tasks      *tasks.Service
	Automation *automation.Service // nil: skills/agents/MCP management is off
	Ops        *ops.Manager        // nil: running project processes is off
	Monitors   *monitor.Service    // nil: health checks are off
	// MCP serves the office tools to agent runs (bearer token per run, no session).
	MCP http.Handler
	// Actions are operations agents proposed; admins approve or reject them.
	Actions *actions.Service
	// Backup writes a copy of the data to a new folder and returns its path.
	Backup func(ctx context.Context) (string, error)
	System SystemInfo
}

// SystemInfo tells the dashboard how this office is installed.
type SystemInfo struct {
	Mode        string `json:"mode"` // local | global
	HomeDir     string `json:"home_dir"`
	ProjectRoot string `json:"project_root,omitempty"`
}

type server struct {
	cfg     Config
	origins map[string]bool
	log     *slog.Logger
}

// New returns the root handler.
func New(cfg Config) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	s := &server{cfg: cfg, origins: map[string]bool{}, log: cfg.Logger}
	for _, o := range cfg.AllowedOrigins {
		s.origins[strings.TrimRight(strings.ToLower(o), "/")] = true
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /api/health", s.healthz) // same, reachable through the dashboard proxy

	mux.HandleFunc("GET /api/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.Handle("GET /api/auth/me", s.requireAuth(http.HandlerFunc(s.me)))
	mux.Handle("POST /api/auth/password", s.requireAuth(http.HandlerFunc(s.changePassword)))

	mux.Handle("GET /api/users", s.requireRole(storage.RoleAdmin, http.HandlerFunc(s.listUsers)))
	mux.Handle("POST /api/users", s.requireRole(storage.RoleAdmin, http.HandlerFunc(s.createUser)))
	mux.Handle("PATCH /api/users/{id}", s.requireRole(storage.RoleAdmin, http.HandlerFunc(s.updateUser)))
	mux.Handle("POST /api/users/{id}/reset-password", s.requireRole(storage.RoleAdmin, http.HandlerFunc(s.resetPassword)))
	mux.Handle("GET /api/audit", s.requireRole(storage.RoleAdmin, http.HandlerFunc(s.listAudit)))

	if cfg.Providers != nil && cfg.Org != nil {
		s.orgRoutes(mux)
	}

	return s.securityHeaders(s.csrf(mux))
}

// ---- middleware ----

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxSession
)

func userFrom(r *http.Request) storage.User { return r.Context().Value(ctxUser).(storage.User) }
func sessionFrom(r *http.Request) storage.Session {
	return r.Context().Value(ctxSession).(storage.Session)
}

func (s *server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// csrf protects cookie-authenticated state changes. A cross-site page cannot
// send application/json without a CORS preflight (which we never grant), and
// a present Origin must be ours. SameSite=Lax on the cookie is the third layer.
func (s *server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(r, origin) {
			writeError(w, http.StatusForbidden, "origin not allowed")
			return
		}
		if r.ContentLength != 0 || r.Header.Get("Content-Type") != "" {
			mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
				return
			}
		}
		limit := int64(maxBody)
		if r.URL.Path == "/api/transfer/import" || strings.HasPrefix(r.URL.Path, "/api/automation/") {
			limit = 8 << 20
		}
		if strings.HasSuffix(r.URL.Path, "/attachments") {
			limit = 15 << 20 // 10 MB file as base64
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

func (s *server) originAllowed(r *http.Request, origin string) bool {
	o := strings.TrimRight(strings.ToLower(origin), "/")
	if s.origins[o] {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	host := r.Host
	if s.trusted(r.RemoteAddr) {
		if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
			host = fh
		}
	}
	return strings.EqualFold(u.Host, host)
}

func (s *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie(SessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		u, sess, err := s.cfg.Auth.Authenticate(r.Context(), ck.Value)
		if errors.Is(err, auth.ErrUnauthenticated) {
			s.clearCookie(w, r)
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, u)
		ctx = orgmodel.WithActor(ctx, "human:"+u.Email)
		ctx = context.WithValue(ctx, ctxSession, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *server) requireRole(role storage.Role, next http.Handler) http.Handler {
	return s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r).Role != role {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// ---- helpers ----

func (s *server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if s.trusted(r.RemoteAddr) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	return host
}

func (s *server) secure(r *http.Request) bool {
	if s.cfg.SecureCookies || r.TLS != nil {
		return true
	}
	return s.trusted(r.RemoteAddr) && r.Header.Get("X-Forwarded-Proto") == "https"
}

func (s *server) setCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: s.secure(r), SameSite: http.SameSiteLaxMode,
	})
}

func (s *server) clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.secure(r), SameSite: http.SameSiteLaxMode,
	})
}

// trusted reports whether the direct peer is a configured proxy.
func (s *server) trusted(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	for _, p := range s.cfg.TrustedProxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("api: internal error", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
