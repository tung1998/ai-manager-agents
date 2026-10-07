package chat

import (
	"slices"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A thinking level goes to Claude Code as --effort in every kind of run, to
// Codex as model_reasoning_effort (max = its xhigh); none, nothing is added.
func TestEffortArgs(t *testing.T) {
	for _, req := range []RunRequest{{Effort: "max"}, {Effort: "max", Write: true}, {Effort: "max", FullAccess: true}, {Effort: "max", NoTools: true}} {
		a := claudeRunner{}.args(req, false)
		if i := slices.Index(a, "--effort"); i < 0 || a[i+1] != "max" {
			t.Fatalf("claude args %+v: %v", req, a)
		}
	}
	if a := (claudeRunner{}).args(RunRequest{}, false); slices.Contains(a, "--effort") {
		t.Fatalf("no level, yet: %v", a)
	}
	if got := strings.Join(codexEffortArgs("max"), " "); got != `-c model_reasoning_effort="xhigh"` {
		t.Fatalf("codex max = %s", got)
	}
	if got := strings.Join(codexEffortArgs("low"), " "); got != `-c model_reasoning_effort="low"` {
		t.Fatalf("codex low = %s", got)
	}
	if codexEffortArgs("") != nil {
		t.Fatal("codex: no level, yet args")
	}
}

// Antigravity takes a level only on its default model (up to high): a named
// model carries its own (gemini-3.8-flash-high) or none, so none is sent.
func TestAntigravityEffort(t *testing.T) {
	agy := storage.ProviderAntigravityCLI
	for _, c := range []struct{ model, in, want string }{
		{"gemini-3.8-flash-high", "max", ""}, {"gemini-3.8-flash-high", "high", ""}, {"claude-sonnet-4-6", "low", ""},
		{"", "max", "high"}, {"", "medium", "medium"}, {"", "", ""},
	} {
		if got := storage.FitEffort(agy, c.model, c.in); got != c.want {
			t.Fatalf("agy %q %q = %q, want %q", c.model, c.in, got, c.want)
		}
	}
	if got := storage.FitEffort(storage.ProviderClaudeCLI, "x", "max"); got != "max" {
		t.Fatalf("claude max = %q", got)
	}
}
