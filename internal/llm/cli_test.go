package llm

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIFailureReadsTheEndOfTheJSON(t *testing.T) {
	// the reason sits after a long usage block, as Claude Code prints it
	out := `{"duration_api_ms":0,"usage":{"input_tokens":0,` + strings.Repeat(`"pad":"x",`, 80) + `"output_tokens":0},"is_error":true,"subtype":"error","result":"Invalid API key · Please run /login"}`
	if got := cliFailure(out, ""); !strings.Contains(got, "/login") {
		t.Fatalf("lost the reason: %q", got)
	}
	if got := cliFailure("", "boom"); got != "boom" {
		t.Fatalf("stderr: %q", got)
	}
}

// fakeBin writes an executable script answering --version and auth status.
func fakeBin(t *testing.T, loggedIn string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "claude")
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo '9.9.9 (Claude Code)';;\nauth) printf '%s' '{\"loggedIn\":" + loggedIn + ",\"email\":\"a@b.c\"}';;\nesac\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClaudeCheckSignedOut(t *testing.T) {
	c := &claudeCLI{bin: fakeBin(t, "false")}
	if _, err := c.Check(context.Background()); !errors.Is(err, ErrNeedsLogin) {
		t.Fatalf("want ErrNeedsLogin, got %v", err)
	}
	c = &claudeCLI{bin: fakeBin(t, "true")}
	if res, err := c.Check(context.Background()); err != nil || res.Version == "" {
		t.Fatalf("signed in: %v %+v", err, res)
	}
}

func TestCodexSessionExpired(t *testing.T) {
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	jwt := func(exp time.Time) string {
		body := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix())))
		return "h." + body + ".s"
	}
	write := func(s string) {
		dir := t.TempDir()
		t.Setenv("CODEX_HOME", dir)
		if s != "" {
			if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(s), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	cases := []struct {
		name, auth string
		want       bool
	}{
		{"no file (keyring)", "", false},
		{"api key", `{"OPENAI_API_KEY":"sk-x"}`, false},
		{"fresh", `{"tokens":{"access_token":"` + jwt(now.Add(48*time.Hour)) + `"},"last_refresh":"2026-10-10T00:00:00Z"}`, false},
		{"expired but recently refreshed", `{"tokens":{"access_token":"` + jwt(now.Add(-time.Hour)) + `"},"last_refresh":"2026-10-01T00:00:00Z"}`, false},
		{"idle for months", `{"tokens":{"access_token":"` + jwt(time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)) + `"},"last_refresh":"2026-04-13T08:22:13.75Z"}`, true},
	}
	for _, c := range cases {
		write(c.auth)
		if got, detail := CodexSessionExpired(CodexHome(), now); got != c.want {
			t.Errorf("%s: got %v (%s), want %v", c.name, got, detail, c.want)
		}
	}
}

func TestCodexFailureReadsTurnFailed(t *testing.T) {
	out := `{"type":"thread.started","thread_id":"x"}
{"type":"turn.started"}
{"type":"error","message":"Reconnecting... 1/5"}
{"type":"turn.failed","error":{"message":"unexpected status 503: auth_unavailable"}}
`
	if got := codexFailure(out); got != "unexpected status 503: auth_unavailable" {
		t.Fatalf("got %q", got)
	}
	if got := codexFailure(`{"type":"error","message":"Missing environment variable"}`); got != "Missing environment variable" {
		t.Fatalf("error only: %q", got)
	}
}

func TestClaudeModelsReadTheHelpAliases(t *testing.T) {
	help := `Options:
  --model <model>                       Model for the current session. Provide
                                        an alias for the latest model (e.g.
                                        'fable', 'opus', or 'sonnet') or a
                                        model's full name.
  -n, --name <name>                     Set a display name ('ignored')
`
	got := mergeModels(helpAliases(help, "--model"), claudeAliases)
	want := []string{"fable", "opus", "sonnet", "haiku"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("models = %v, want %v", got, want)
	}
	if got := mergeModels(helpAliases("no such flag", "--model"), claudeAliases); fmt.Sprint(got) != fmt.Sprint(claudeAliases) {
		t.Fatalf("fallback = %v", got)
	}
}
