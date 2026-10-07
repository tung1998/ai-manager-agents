package clitools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeClaude behaves like `claude` for --version, auth status and auth login.
const fakeClaude = `#!/bin/sh
STATE="$(dirname "$0")/.logged-in"
case "$1 $2" in
  "--version "*) echo "9.9.9 (Claude Code)";;
  "auth status")
    if [ -f "$STATE" ]; then echo '{"loggedIn":true,"authMethod":"claude.ai","email":"a@b.c","orgName":"Acme"}'
    else echo '{"loggedIn":false}'; exit 1; fi;;
  "auth login")
    echo "Opening browser to sign in…"
    echo "If the browser did not open, visit: https://claude.ai/oauth/authorize?code=true&state=xyz"
    printf "Paste code here if prompted > "
    read CODE
    if [ "$CODE" = "good-code" ]; then touch "$STATE"; echo "Login successful."; else echo "Invalid code"; exit 1; fi;;
esac
`

func testManager(t *testing.T) (*Manager, string) {
	dir := t.TempDir()
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	env = append(env, "PATH="+dir+":/usr/bin:/bin")
	m := &Manager{jobs: map[string]*Job{}, last: map[string]*Job{}, env: env, tools: tools}
	return m, dir
}

func writeBin(t *testing.T, dir, name, script string) {
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func waitState(t *testing.T, j *Job, want string) JobView {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if v := j.Snapshot(); v.State == want {
			return v
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job state = %s, want %s; output:\n%s", j.Snapshot().State, want, j.Snapshot().Output)
	return JobView{}
}

func TestStatusAndLogin(t *testing.T) {
	m, dir := testManager(t)
	ctx := context.Background()

	s, _ := m.Status(ctx, "claude")
	if s.Installed {
		t.Fatal("claude should not be installed yet")
	}
	if _, err := m.Login("claude"); err != ErrNotInstalled {
		t.Fatalf("login before install err = %v", err)
	}

	writeBin(t, dir, "claude", fakeClaude)
	s, _ = m.Status(ctx, "claude")
	if !s.Installed || s.Version != "9.9.9 (Claude Code)" || s.Auth.LoggedIn {
		t.Fatalf("status = %+v", s)
	}

	j, err := m.Login("claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Login("claude"); err != ErrBusy {
		t.Fatalf("second login err = %v", err)
	}
	// wait for the URL to appear, then paste the code
	deadline := time.Now().Add(5 * time.Second)
	for len(j.Snapshot().URLs) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	v := j.Snapshot()
	if len(v.URLs) != 1 || !strings.HasPrefix(v.URLs[0], "https://claude.ai/oauth/authorize") {
		t.Fatalf("urls = %v, output = %q", v.URLs, v.Output)
	}
	if err := j.Input("good-code"); err != nil {
		t.Fatal(err)
	}
	v = waitState(t, j, "succeeded")
	if strings.Contains(v.Output, "\x1b") || !strings.Contains(v.Output, "Login successful.") {
		t.Fatalf("output = %q", v.Output)
	}
	s, _ = m.Status(ctx, "claude")
	if !s.Auth.LoggedIn || s.Auth.Account != "a@b.c · Acme" || s.Job == nil || s.Job.State != "succeeded" {
		t.Fatalf("after login = %+v", s)
	}
}

func TestInstallAndCancel(t *testing.T) {
	m, dir := testManager(t)
	// a fake "brew" that installs a fake codex
	writeBin(t, dir, "brew", "#!/bin/sh\necho \"==> Installing $3\"\nprintf '#!/bin/sh\\necho codex-cli 1.0.0\\n' > \""+dir+"/codex\"\nchmod +x \""+dir+"/codex\"\necho done\n")
	if _, err := m.Install("codex", "npm"); err != ErrUnknownMethod {
		t.Fatalf("npm missing err = %v", err)
	}
	if _, err := m.Install("codex", "rm -rf /"); err != ErrUnknownMethod {
		t.Fatalf("arbitrary method err = %v", err)
	}
	j, err := m.Install("codex", "brew")
	if err != nil {
		t.Fatal(err)
	}
	v := waitState(t, j, "succeeded")
	if v.Command != "brew install --cask codex" && !strings.HasSuffix(v.Command, "brew install --cask codex") {
		t.Fatalf("command = %q", v.Command)
	}
	s, _ := m.Status(context.Background(), "codex")
	if !s.Installed || s.Version != "codex-cli 1.0.0" {
		t.Fatalf("codex status = %+v", s)
	}

	writeBin(t, dir, "claude", "#!/bin/sh\nif [ \"$1\" = auth ] && [ \"$2\" = login ]; then sleep 30; fi\necho '{}'\n")
	lj, err := m.Login("claude")
	if err != nil {
		t.Fatal(err)
	}
	lj.Cancel()
	waitState(t, lj, "cancelled")
}

func TestCleanOutput(t *testing.T) {
	got := cleanOutput("\x1b[1mHello\x1b[0m\r\n\n\n\nWorld  \n")
	if got != "Hello\n\nWorld" {
		t.Fatalf("clean = %q", got)
	}
}

func TestCodexStatusTrustsCLI(t *testing.T) {
	m, dir := testManager(t)
	writeBin(t, dir, "codex", "#!/bin/sh\ncase \"$1 $2\" in \"--version \"*) echo codex-cli 1;; \"login status\") echo 'Not logged in'; exit 1;; esac\n")
	s, _ := m.Status(context.Background(), "codex")
	if s.Auth.LoggedIn {
		t.Fatalf("explicit 'Not logged in' must win over any credentials file: %+v", s.Auth)
	}
	writeBin(t, dir, "codex", "#!/bin/sh\ncase \"$1 $2\" in \"--version \"*) echo codex-cli 1;; \"login status\") echo 'Logged in using ChatGPT';; esac\n")
	if s, _ := m.Status(context.Background(), "codex"); !s.Auth.LoggedIn {
		t.Fatalf("logged in: %+v", s.Auth)
	}
}

// fakeGemini acts like `gemini` signing in with NO_BROWSER: a link and a code
// prompt, a fresh link after a wrong code, then its chat (which never ends).
const fakeGemini = `#!/bin/sh
[ "$1" = "--version" ] && { echo "0.63.0"; exit 0; }
[ "$NO_BROWSER" = "true" ] || { echo "no NO_BROWSER"; exit 1; }
n=1
while :; do
  echo "Please visit the following URL to authorize the application:"
  echo "https://accounts.google.com/o/oauth2/v2/auth?state=s$n&client_id=x"
  printf "Enter the authorization code: "
  read CODE
  if [ "$CODE" = "good-code" ]; then
    mkdir -p "$GEMINI_CLI_HOME/.gemini"
    echo '{}' > "$GEMINI_CLI_HOME/.gemini/oauth_creds.json"
    echo '{"active":"me@gmail.com","old":[]}' > "$GEMINI_CLI_HOME/.gemini/google_accounts.json"
    echo "Gemini CLI ready"
    sleep 600
  fi
  echo "Failed to authenticate with authorization code:invalid_grant"
  n=$((n+1))
done
`

func TestGeminiLogin(t *testing.T) {
	m, dir := testManager(t)
	home := t.TempDir()
	t.Setenv("GEMINI_CLI_HOME", home)
	m.env = append(m.env, "GEMINI_CLI_HOME="+home)
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_GENAI_USE_GCA", "GOOGLE_CLOUD_ACCESS_TOKEN"} {
		t.Setenv(k, "")
	}
	writeBin(t, dir, "gemini", fakeGemini)
	ctx := context.Background()
	if s, _ := m.Status(ctx, "gemini"); !s.Installed || s.Version != "0.63.0" || s.Auth.LoggedIn {
		t.Fatalf("status = %+v", s)
	}
	j, err := m.Login("gemini")
	if err != nil {
		t.Fatal(err)
	}
	// sign in with Google is picked so the CLI prints the link, not a menu
	raw, _ := os.ReadFile(filepath.Join(home, ".gemini", "settings.json"))
	if !strings.Contains(string(raw), `"selectedType": "oauth-personal"`) {
		t.Fatalf("settings = %s", raw)
	}
	waitURL := func(state string) {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if v := j.Snapshot(); len(v.URLs) == 1 && strings.Contains(v.URLs[0], "state="+state) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("want one link with state=%s; urls = %v", state, j.Snapshot().URLs)
	}
	waitURL("s1")
	// a wrong code: the newest link replaces the old one
	if err := j.Input("bad"); err != nil {
		t.Fatal(err)
	}
	waitURL("s2")
	if err := j.Input("good-code"); err != nil {
		t.Fatal(err)
	}
	// signed in: the job ends although the CLI goes on into its chat
	waitState(t, j, "succeeded")
	s, _ := m.Status(ctx, "gemini")
	if !s.Auth.LoggedIn || s.Auth.Account != "me@gmail.com" {
		t.Fatalf("after login = %+v", s.Auth)
	}
}

func TestGeminiPrepareLoginKeepsChoice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GEMINI_CLI_HOME", home)
	os.MkdirAll(filepath.Join(home, ".gemini"), 0o700)
	keep := `{"security":{"auth":{"selectedType":"gemini-api-key"}},"ui":{"theme":"x"}}`
	os.WriteFile(filepath.Join(home, ".gemini", "settings.json"), []byte(keep), 0o600)
	if err := geminiPrepareLogin(); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(home, ".gemini", "settings.json")); string(raw) != keep {
		t.Fatalf("an existing choice was rewritten: %s", raw)
	}
}

func TestInstallDoesNotAsk(t *testing.T) {
	m, dir := testManager(t)
	m.env = append(m.env, "HOMEBREW_ASK=1")
	// a brew that asks unless HOMEBREW_NO_ASK (Homebrew 6+), or when HOMEBREW_ASK (older)
	writeBin(t, dir, "brew", `#!/bin/sh
if [ -z "$HOMEBREW_NO_ASK" ] || [ -n "$HOMEBREW_ASK" ]; then printf "Do you want to proceed with the installation? [y/n] "; read A; fi
echo installed
`)
	j, err := m.Install("gemini", "brew")
	if err != nil {
		t.Fatal(err)
	}
	if v := waitState(t, j, "succeeded"); strings.Contains(v.Output, "[y/n]") {
		t.Fatalf("output = %q", v.Output)
	}
}

func TestNodeBinsNewestNvm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NVM_BIN", "")
	for _, v := range []string{"v9.11.2", "v22.23.2", "v20.1.0"} {
		os.MkdirAll(filepath.Join(home, ".nvm", "versions", "node", v, "bin"), 0o755)
	}
	if got := nodeBins(home); len(got) != 1 || !strings.Contains(got[0], "v22.23.2") {
		t.Fatalf("nodeBins = %v", got)
	}
	t.Setenv("NVM_BIN", "/active/bin")
	if got := nodeBins(home); len(got) != 1 || got[0] != "/active/bin" {
		t.Fatalf("with NVM_BIN = %v", got)
	}
}
