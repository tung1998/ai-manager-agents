package clitools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeClaudeHome keeps its sign-in in CLAUDE_CONFIG_DIR, like Claude Code.
const fakeClaudeHome = `#!/bin/sh
STATE="${CLAUDE_CONFIG_DIR:-$(dirname "$0")}/.logged-in"
case "$1 $2" in
  "--version "*) echo "9.9.9 (Claude Code)";;
  "auth status")
    if [ -f "$STATE" ]; then echo '{"loggedIn":true,"email":"two@b.c"}'
    else echo '{"loggedIn":false}'; exit 1; fi;;
  "auth login")
    echo "visit: https://claude.ai/oauth/authorize?code=true"
    read CODE
    touch "$STATE"; echo "Login successful.";;
  "auth logout") rm -f "$STATE";;
esac
`

func TestSecondAccount(t *testing.T) {
	m, dir := testManager(t)
	ctx := context.Background()
	writeBin(t, dir, "claude", fakeClaudeHome)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // the machine's config, for PrepareHome
	home := filepath.Join(t.TempDir(), "prv_2")

	j, err := m.LoginIn("claude", home)
	if err != nil {
		t.Fatal(err)
	}
	// the machine's account is another job: it can sign in meanwhile
	if j2, err := m.Login("claude"); err != nil {
		t.Fatalf("machine login during second account's = %v", err)
	} else {
		j2.Cancel()
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(j.Snapshot().URLs) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if err := j.Input("code"); err != nil {
		t.Fatal(err)
	}
	waitState(t, j, "succeeded")

	if s, _ := m.StatusIn(ctx, "claude", home); !s.Auth.LoggedIn || s.Auth.Account != "two@b.c" || s.Job == nil {
		t.Fatalf("second account = %+v", s)
	}
	if s, _ := m.Status(ctx, "claude"); s.Auth.LoggedIn {
		t.Fatal("signing in the second account signed in the machine's")
	}
	if err := m.LogoutIn(ctx, "claude", home); err != nil {
		t.Fatal(err)
	}
	if s, _ := m.StatusIn(ctx, "claude", home); s.Auth.LoggedIn {
		t.Fatal("still signed in after logout")
	}
	if _, err := m.LoginIn("antigravity", home); err != ErrNoHome {
		t.Fatalf("antigravity second account err = %v", err)
	}
}

func TestPrepareHome(t *testing.T) {
	src := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", src)
	for _, f := range []string{"settings.json", ".credentials.json", "CLAUDE.md", "policy-limits.json"} {
		if err := os.WriteFile(filepath.Join(src, f), []byte(f), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"skills", "projects", "backups"} {
		if err := os.Mkdir(filepath.Join(src, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(t.TempDir(), "prv_2")
	if err := PrepareHome("claude", home); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"settings.json", "CLAUDE.md", "skills", "projects"} {
		if to, err := os.Readlink(filepath.Join(home, f)); err != nil || to != filepath.Join(src, f) {
			t.Fatalf("%s → %q, %v", f, to, err)
		}
	}
	// what belongs to the machine's account stays out
	for _, f := range []string{".credentials.json", "policy-limits.json", "backups"} {
		if _, err := os.Lstat(filepath.Join(home, f)); !os.IsNotExist(err) {
			t.Fatalf("%s was shared", f)
		}
	}
	// what appears later is linked on the next run; what the account wrote stays
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte("own"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "RTK.md"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareHome("claude", home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(filepath.Join(home, "RTK.md")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".claude.json")); !strings.EqualFold(string(b), "own") {
		t.Fatal("the account's own file was replaced")
	}
}

// Codex and Gemini CLI: the setup is shared, the credentials are not.
func TestPrepareHomeCodexGemini(t *testing.T) {
	write := func(dir string, files ...string) {
		for _, f := range files {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, f), []byte(f), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	codex := t.TempDir()
	t.Setenv("CODEX_HOME", codex)
	write(codex, "config.toml", "AGENTS.md", "auth.json", "state_5.sqlite")
	home := filepath.Join(t.TempDir(), "prv_c")
	if err := PrepareHome("codex", home); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"config.toml", "AGENTS.md"} {
		if to, err := os.Readlink(filepath.Join(home, f)); err != nil || to != filepath.Join(codex, f) {
			t.Fatalf("codex %s → %q, %v", f, to, err)
		}
	}
	for _, f := range []string{"auth.json", "state_5.sqlite"} {
		if _, err := os.Lstat(filepath.Join(home, f)); !os.IsNotExist(err) {
			t.Fatalf("codex %s was shared", f)
		}
	}

	gem := t.TempDir()
	t.Setenv("GEMINI_CLI_HOME", gem)
	write(filepath.Join(gem, ".gemini"), "settings.json", "GEMINI.md", "oauth_creds.json", "google_accounts.json")
	home = filepath.Join(t.TempDir(), "prv_g")
	if err := PrepareHome("gemini", home); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".gemini")
	if to, err := os.Readlink(filepath.Join(dir, "GEMINI.md")); err != nil || to != filepath.Join(gem, ".gemini", "GEMINI.md") {
		t.Fatalf("GEMINI.md → %q, %v", to, err)
	}
	// a copy: signing in writes the method there, not in the machine's
	if st, err := os.Lstat(filepath.Join(dir, "settings.json")); err != nil || st.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("settings.json = %v, %v", st, err)
	}
	for _, f := range []string{"oauth_creds.json", "google_accounts.json"} {
		if _, err := os.Lstat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Fatalf("gemini %s was shared", f)
		}
	}
	if err := PrepareHome("antigravity", home); err != ErrNoHome {
		t.Fatalf("antigravity err = %v", err)
	}
}
