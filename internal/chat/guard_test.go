package chat

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func guardSays(t *testing.T, ev map[string]any, userMCP bool) string {
	t.Helper()
	b, _ := json.Marshal(ev)
	var out bytes.Buffer
	Guard(bytes.NewReader(b), &out, userMCP)
	return out.String()
}

// Edits stay in the working folder, .claude/skills included; MCP is office's own
// unless the agent may use the person's servers.
func TestGuard(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	in := func(p string) map[string]any {
		return map[string]any{"cwd": dir, "tool_name": "Write", "tool_input": map[string]any{"file_path": p}}
	}
	for _, p := range []string{filepath.Join(dir, ".claude/skills/x/SKILL.md"), "src/a.go", filepath.Join(dir, "new/deep/b.md")} {
		if s := guardSays(t, in(p), false); s != "" {
			t.Errorf("%s denied: %s", p, s)
		}
	}
	for _, p := range []string{filepath.Join(filepath.Dir(dir), "out.txt"), "../out.txt", "/etc/hosts", filepath.Join(dir, "src/../../x")} {
		if s := guardSays(t, in(p), false); !strings.Contains(s, `"deny"`) {
			t.Errorf("%s allowed", p)
		}
	}
	os.Symlink("/etc", filepath.Join(dir, "link"))
	if s := guardSays(t, in(filepath.Join(dir, "link/hosts")), false); !strings.Contains(s, `"deny"`) {
		t.Error("a symlink out of the folder is allowed")
	}
	mcp := func(name string) map[string]any { return map[string]any{"cwd": dir, "tool_name": name} }
	if s := guardSays(t, mcp("mcp__jira__search"), false); !strings.Contains(s, `"deny"`) {
		t.Error("user MCP allowed without the capability")
	}
	if s := guardSays(t, mcp("mcp__jira__search"), true); s != "" {
		t.Error("user MCP denied with the capability")
	}
	if s := guardSays(t, mcp("mcp__office__propose_change"), false); s != "" {
		t.Errorf("office MCP denied: %s", s)
	}
}

// With the guard, a chat that edits runs under bypassPermissions (Claude Code
// lets it write .claude/skills) and every edit goes through the hook.
func TestClaudeArgsGuard(t *testing.T) {
	var r claudeRunner
	old := GuardCommand
	defer func() { GuardCommand = old }()
	GuardCommand = ""
	if a := r.args(RunRequest{Write: true}, false); a[slices.Index(a, "--permission-mode")+1] != "dontAsk" {
		t.Fatalf("no guard, still dontAsk: %v", a)
	}
	GuardCommand = "'/x/office' hook guard"
	a := r.args(RunRequest{Write: true, DenyPaths: []string{"secrets/"}}, false)
	if a[slices.Index(a, "--permission-mode")+1] != "bypassPermissions" {
		t.Fatalf("mode: %v", a)
	}
	i := slices.Index(a, "--settings")
	if i < 0 || !strings.Contains(a[i+1], `hook guard`) || !strings.Contains(a[i+1], "Write|Edit") {
		t.Fatalf("guard hook missing: %v", a)
	}
	if d := a[slices.Index(a, "--disallowedTools")+1]; !strings.Contains(d, "Edit(./secrets/**)") || !strings.Contains(d, "Edit(./.env)") {
		t.Fatalf("deny rules lost: %s", d)
	}
	if t2 := a[slices.Index(a, "--tools")+1]; strings.Contains(t2, "Bash") {
		t.Fatalf("tools: %s", t2)
	}
	if b := r.args(RunRequest{}, false); b[slices.Index(b, "--permission-mode")+1] != "dontAsk" {
		t.Fatal("a read-only chat must stay dontAsk")
	}
}
