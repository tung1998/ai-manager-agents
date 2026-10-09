package provider_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

func setup(t *testing.T) (*provider.Service, storage.Store, *httptest.Server) {
	dir := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(dir, "o.db"))
	t.Cleanup(func() { st.Close() })
	st.Migrate(context.Background())
	box, err := secrets.Load(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-ant-good-key-1234" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"invalid x-api-key"}}`))
			return
		}
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[{"id":"claude-sonnet-5"}]}`))
			return
		}
		w.Write([]byte(`{"model":"claude-sonnet-5","content":[{"type":"text","text":"xin chào"}],"usage":{"input_tokens":3,"output_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return provider.NewService(st, box, llm.Options{}), st, srv
}

func ptr(s string) *string { return &s }

func TestCreateEncryptsAndTests(t *testing.T) {
	svc, st, srv := setup(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, provider.Input{Name: "x", Kind: storage.ProviderAnthropic}); !errors.Is(err, provider.ErrNeedsKey) {
		t.Fatalf("missing key err = %v", err)
	}
	p, err := svc.Create(ctx, provider.Input{Name: "Claude", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: ptr("sk-ant-good-key-1234")})
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsDefault || p.APIKeyHint != "…1234" || strings.Contains(p.APIKeyEnc, "good-key") || p.TierModels["strong"] == "" {
		t.Fatalf("created = %+v", p)
	}
	res, err := svc.Test(ctx, p.ID, "chào", "")
	if err != nil || !res.OK || res.Response == nil || res.Response.Text != "xin chào" {
		t.Fatalf("test = %+v, %v", res, err)
	}
	got, _ := st.Providers().Get(ctx, p.ID)
	if got.Status != "ok" || len(got.Models) != 1 {
		t.Fatalf("status not saved: %+v", got)
	}

	// update without key keeps it; wrong key marks error
	if _, err := svc.Update(ctx, p.ID, provider.Input{Name: "Claude API", Kind: storage.ProviderAnthropic, BaseURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	if res, _ := svc.Test(ctx, p.ID, "", ""); !res.OK {
		t.Fatalf("key lost on update: %+v", res)
	}
	svc.Update(ctx, p.ID, provider.Input{Name: "Claude API", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: ptr("sk-ant-bad-key-99999")})
	res, _ = svc.Test(ctx, p.ID, "", "")
	if res.OK || !strings.Contains(res.Detail, "invalid x-api-key") {
		t.Fatalf("bad key result = %+v", res)
	}
	if got, _ := st.Providers().Get(ctx, p.ID); got.Status != "error" {
		t.Fatalf("status = %s", got.Status)
	}

	// env var key
	t.Setenv("TEST_ANTHROPIC_KEY", "sk-ant-good-key-1234")
	e, err := svc.Create(ctx, provider.Input{Name: "Env", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKeyEnv: "TEST_ANTHROPIC_KEY"})
	if err != nil || e.IsDefault {
		t.Fatalf("env provider = %+v, %v", e, err)
	}
	if res, _ := svc.Test(ctx, e.ID, "", ""); !res.OK {
		t.Fatalf("env key test = %+v", res)
	}
	if _, err := svc.Create(ctx, provider.Input{Name: "Env", Kind: storage.ProviderClaudeCLI}); !errors.Is(err, provider.ErrNameTaken) {
		t.Fatalf("dup name err = %v", err)
	}
}

func TestResolveModel(t *testing.T) {
	svc, _, srv := setup(t)
	ctx := context.Background()
	def, _ := svc.Create(ctx, provider.Input{Name: "Claude", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: ptr("sk-ant-good-key-1234")})
	other, _ := svc.Create(ctx, provider.Input{Name: "Local", Kind: storage.ProviderOpenAICompatible, BaseURL: "http://x/v1",
		TierModels: map[string]string{"fast": "llama-small"}})

	p, m, err := svc.ResolveModel(ctx, storage.Agent{ModelTier: "strong"})
	if err != nil || p.ID != def.ID || m != "claude-opus-5-5" {
		t.Fatalf("default tier = %s %s %v", p.Name, m, err)
	}
	p, m, _ = svc.ResolveModel(ctx, storage.Agent{ProviderID: other.ID, ModelTier: "fast"})
	if p.ID != other.ID || m != "llama-small" {
		t.Fatalf("agent provider tier = %s %s", p.Name, m)
	}
	_, m, _ = svc.ResolveModel(ctx, storage.Agent{ModelTier: "fast", LLMModel: "claude-sonnet-5"})
	if m != "claude-sonnet-5" {
		t.Fatalf("explicit model = %s", m)
	}
}

func TestTestFillsTiersFromModels(t *testing.T) {
	_, st, _ := setup(t)
	dir := t.TempDir()
	box, _ := secrets.Load(filepath.Join(dir, "k"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"gpt-5"},{"id":"gpt-5-mini"},{"id":"text-embedding-3-small"}]}`))
	}))
	defer srv.Close()
	svc := provider.NewService(st, box, llm.Options{})
	p, err := svc.Create(context.Background(), provider.Input{Name: "GPT", Kind: storage.ProviderOpenAI, BaseURL: srv.URL, APIKey: ptr("sk-openai-test-1234")})
	if err != nil {
		t.Fatal(err)
	}
	if res, _ := svc.Test(context.Background(), p.ID, "", ""); !res.OK {
		t.Fatalf("test = %+v", res)
	}
	got, _ := st.Providers().Get(context.Background(), p.ID)
	if got.TierModels["strong"] != "gpt-5" || got.TierModels["fast"] != "gpt-5-mini" {
		t.Fatalf("tiers = %v", got.TierModels)
	}
}

// Re-pointing a connection at another host must not carry the stored key
// along: otherwise editing the URL is enough to exfiltrate it.
func TestBaseURLChangeDropsStoredKey(t *testing.T) {
	svc, st, srv := setup(t)
	ctx := context.Background()
	p, _ := svc.Create(ctx, provider.Input{Name: "Claude", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: ptr("sk-ant-good-key-1234")})

	if _, err := svc.Update(ctx, p.ID, provider.Input{Name: "Claude", Kind: storage.ProviderAnthropic, BaseURL: "https://evil.example"}); !errors.Is(err, provider.ErrKeyRequiredForNewURL) {
		t.Fatalf("url change without key err = %v", err)
	}
	got, _ := st.Providers().Get(ctx, p.ID)
	if got.BaseURL != srv.URL || got.APIKeyEnc == "" {
		t.Fatalf("rejected update must not change anything: %+v", got)
	}
	// same URL keeps the key; new URL with a new key is fine
	if _, err := svc.Update(ctx, p.ID, provider.Input{Name: "Claude 2", Kind: storage.ProviderAnthropic, BaseURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, p.ID, provider.Input{Name: "Claude 2", Kind: storage.ProviderAnthropic, BaseURL: srv.URL + "/", APIKey: ptr("sk-ant-good-key-1234")}); err != nil {
		t.Fatal(err)
	}
	// a keyless compatible endpoint can move freely
	c, _ := svc.Create(ctx, provider.Input{Name: "Local", Kind: storage.ProviderOpenAICompatible, BaseURL: "http://a/v1"})
	if _, err := svc.Update(ctx, c.ID, provider.Input{Name: "Local", Kind: storage.ProviderOpenAICompatible, BaseURL: "http://b/v1"}); err != nil {
		t.Fatalf("keyless move = %v", err)
	}
}

func TestCLIProviderUsesBinResolver(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\necho '9.9.9 (Claude Code)'\n"), 0o755)
	p, _ := svc.Create(ctx, provider.Input{Name: "CC", Kind: storage.ProviderClaudeCLI})
	svc.SetBinResolver(func(name string) string {
		if name == "claude" {
			return bin
		}
		return ""
	})
	if res, _ := svc.Test(ctx, p.ID, "", ""); !res.OK || res.Version != "9.9.9 (Claude Code)" {
		t.Fatalf("test via resolver = %+v", res)
	}
}

func TestGeminiCLIProvider(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("GEMINI_CLI_HOME", home)
	t.Setenv("GEMINI_API_KEY", "")
	bin := filepath.Join(t.TempDir(), "gemini")
	os.WriteFile(bin, []byte("#!/bin/sh\necho '0.63.0'\n"), 0o755)
	svc.SetBinResolver(func(name string) string {
		if name == "gemini" {
			return bin
		}
		return ""
	})
	// the kind is stored (no CHECK left to refuse it)
	p, err := svc.Create(ctx, provider.Input{Name: "Gemini", Kind: storage.ProviderGeminiCLI,
		TierModels: map[string]string{"strong": "pro", "balanced": "flash", "fast": "flash-lite"}})
	if err != nil {
		t.Fatal(err)
	}
	if res, _ := svc.Test(ctx, p.ID, "", ""); res.OK || !res.NeedsLogin || res.CLITool != "gemini" {
		t.Fatalf("signed out = %+v", res)
	}
	os.MkdirAll(filepath.Join(home, ".gemini"), 0o700)
	os.WriteFile(filepath.Join(home, ".gemini", "oauth_creds.json"), []byte("{}"), 0o600)
	if res, _ := svc.Test(ctx, p.ID, "", ""); !res.OK || res.Version != "0.63.0" {
		t.Fatalf("signed in = %+v", res)
	}
}

func TestAntigravityCLIProvider(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()
	dir := t.TempDir()
	bin := filepath.Join(dir, "agy")
	os.WriteFile(bin, []byte(`#!/bin/sh
case "$1" in
  --version) echo "1.3.1";;
  models) if [ -f "`+dir+`/in" ]; then echo "Fetching available models..."; echo "gemini-3.8-flash-high"; echo "gemini-3.5-pro (default)"
          else echo "Error: Please sign in to view available models. Launch the CLI without arguments to sign in."; exit 1; fi;;
esac
`), 0o755)
	svc.SetBinResolver(func(name string) string {
		if name == "agy" {
			return bin
		}
		return ""
	})
	p, err := svc.Create(ctx, provider.Input{Name: "AGY", Kind: storage.ProviderAntigravityCLI})
	if err != nil {
		t.Fatal(err)
	}
	if res, _ := svc.Test(ctx, p.ID, "", ""); res.OK || !res.NeedsLogin || res.CLITool != "antigravity" {
		t.Fatalf("signed out = %+v", res)
	}
	os.WriteFile(filepath.Join(dir, "in"), nil, 0o600)
	res, _ := svc.Test(ctx, p.ID, "", "")
	if !res.OK || res.Version != "1.3.1" || strings.Join(res.Models, ",") != "gemini-3.8-flash-high,gemini-3.5-pro" {
		t.Fatalf("signed in = %+v", res)
	}
}

// A turned-off connection is skipped (the own one too); a fallback entry may
// name the same connection with another model.
func TestChainSkipsOffAndKeepsModels(t *testing.T) {
	svc, st, srv := setup(t)
	ctx := context.Background()
	def, _ := svc.Create(ctx, provider.Input{Name: "Claude", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: ptr("sk-ant-good-key-1234")})
	other, _ := svc.Create(ctx, provider.Input{Name: "Local", Kind: storage.ProviderOpenAICompatible, BaseURL: "http://x/v1",
		TierModels: map[string]string{"fast": "llama-small"}})
	a := storage.Agent{ModelTier: "fast", LLMModel: "claude-sonnet-5", FallbackProviderIDs: []string{
		storage.FallbackEntry(def.ID, "claude-haiku-4-5"), storage.FallbackEntry(def.ID, "claude-sonnet-5"), other.ID}}
	name := func(cs []provider.Choice) string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Provider.Name+":"+c.Model)
		}
		return strings.Join(out, ",")
	}
	if cs, err := svc.Chain(ctx, a); err != nil || name(cs) != "Claude:claude-sonnet-5,Claude:claude-haiku-4-5,Local:llama-small" {
		t.Fatalf("chain = %s %v", name(cs), err)
	}
	def.Enabled = false
	st.Providers().Update(ctx, def)
	if cs, err := svc.Chain(ctx, a); err != nil || name(cs) != "Local:llama-small" {
		t.Fatalf("own off = %s %v", name(cs), err)
	}
	other.Enabled = false
	st.Providers().Update(ctx, other)
	if _, err := svc.Chain(ctx, a); !errors.Is(err, provider.ErrAllOff) {
		t.Fatalf("all off = %v", err)
	}
}

// Deleting the default connection must fail: every agent with no ProviderID
// of its own resolves through Default() and would break on every run.
func TestDeleteRefusesDefault(t *testing.T) {
	svc, st, srv := setup(t)
	ctx := context.Background()
	def, _ := svc.Create(ctx, provider.Input{Name: "Claude", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: ptr("sk-ant-good-key-1234")})
	if err := svc.Delete(ctx, def.ID); !errors.Is(err, provider.ErrIsDefault) {
		t.Fatalf("delete default err = %v, want ErrIsDefault", err)
	}
	if _, err := st.Providers().Get(ctx, def.ID); err != nil {
		t.Fatalf("default connection was deleted: %v", err)
	}
}

// Deleting a non-default connection an agent points to must succeed: the
// agent is unlinked (ON DELETE SET NULL) and keeps running on the default.
func TestDeleteNonDefaultUnlinksAgent(t *testing.T) {
	svc, st, srv := setup(t)
	ctx := context.Background()
	def, _ := svc.Create(ctx, provider.Input{Name: "Claude", Kind: storage.ProviderAnthropic, BaseURL: srv.URL, APIKey: ptr("sk-ant-good-key-1234")})
	other, _ := svc.Create(ctx, provider.Input{Name: "Local", Kind: storage.ProviderOpenAICompatible, BaseURL: "http://x/v1"})
	repo, err := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: "/code/shop"})
	if err != nil {
		t.Fatal(err)
	}
	own, err := st.Agents().Create(ctx, storage.Agent{ProjectID: repo.ID, Key: "own", Name: "Own", ModelTier: "fast", ProviderID: other.ID})
	if err != nil {
		t.Fatal(err)
	}
	fb, err := st.Agents().Create(ctx, storage.Agent{ProjectID: repo.ID, Key: "fb", Name: "Fb", ModelTier: "fast", FallbackProviderIDs: []string{other.ID}})
	if err != nil {
		t.Fatal(err)
	}

	agents, err := svc.AgentsUsing(ctx, other.ID)
	if err != nil || len(agents) != 2 {
		t.Fatalf("AgentsUsing = %v, %v", agents, err)
	}

	if err := svc.Delete(ctx, other.ID); err != nil {
		t.Fatalf("delete non-default: %v", err)
	}
	if _, err := st.Providers().Get(ctx, other.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("provider still exists: %v", err)
	}
	gotOwn, err := st.Agents().Get(ctx, own.ID)
	if err != nil || gotOwn.ProviderID != "" {
		t.Fatalf("own agent not unlinked: %+v, %v", gotOwn, err)
	}
	_ = fb
	// def is still the default, so the unlinked agent still resolves.
	if p, _, err := svc.ResolveModel(ctx, gotOwn); err != nil || p.ID != def.ID {
		t.Fatalf("ResolveModel after unlink = %+v, %v", p, err)
	}
}
