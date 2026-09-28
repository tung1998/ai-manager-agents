package chat

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// The user's own MCP servers (their Claude Code setup) are allowed only when
// the agent has the capability: a PreToolUse hook allows mcp__* under dontAsk.
func TestClaudeArgsUserMCP(t *testing.T) {
	var r claudeRunner
	off := r.args(RunRequest{}, false)
	if slices.Contains(off, "--settings") {
		t.Fatalf("no capability, no hook: %v", off)
	}
	on := r.args(RunRequest{UserMCP: true}, false)
	i := slices.Index(on, "--settings")
	if i < 0 || i+1 >= len(on) {
		t.Fatalf("hook missing: %v", on)
	}
	var s struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string
				Hooks   []struct{ Type, Command string }
			}
		}
	}
	if err := json.Unmarshal([]byte(on[i+1]), &s); err != nil || len(s.Hooks.PreToolUse) != 1 {
		t.Fatalf("settings are not valid JSON: %v %s", err, on[i+1])
	}
	h := s.Hooks.PreToolUse[0]
	if h.Matcher != "mcp__.*" || len(h.Hooks) != 1 || !strings.Contains(h.Hooks[0].Command, `"permissionDecision":"allow"`) {
		t.Fatalf("hook = %+v", h)
	}
	if !slices.Contains(on, "dontAsk") {
		t.Fatal("everything else stays denied")
	}
}
