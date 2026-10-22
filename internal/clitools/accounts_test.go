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
	if _, err := m.LoginIn("codex", home); err != ErrNoHome {
		t.Fatalf("codex second account err = %v", err)
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
	if err := PrepareHome(home); err != nil {
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
	if err := PrepareHome(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(filepath.Join(home, "RTK.md")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".claude.json")); !strings.EqualFold(string(b), "own") {
		t.Fatal("the account's own file was replaced")
	}
}
