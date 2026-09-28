package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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

// Claude Code reports the subscription's usage windows and each call's
// context; office keeps the latest (shown next to the chat and in the sidebar).
func TestClaudeReportsLimitsAndContext(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.19,"resetsAt":1790593200},"seven_day":{"utilization":0.18,"resetsAt":1790902800},"seven_day_fable":{"utilization":0.04,"resetsAt":1790902800}}}}`,
		`{"type":"assistant","message":{"model":"claude-opus-5-5","usage":{"input_tokens":10,"cache_read_input_tokens":20000,"cache_creation_input_tokens":5000,"output_tokens":7},"content":[]}}`,
		`{"type":"assistant","message":{"model":"claude-opus-5-5","usage":{"input_tokens":12,"cache_read_input_tokens":30000,"cache_creation_input_tokens":1000,"output_tokens":9},"content":[]}}`,
		`{"type":"result","subtype":"success","result":"ok","usage":{"input_tokens":22,"output_tokens":16},"modelUsage":{"claude-haiku-4-5":{"contextWindow":200000},"claude-opus-5-5":{"contextWindow":1000000}}}`,
	}
	fixture := filepath.Join(dir, "out.jsonl")
	os.WriteFile(fixture, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	bin := filepath.Join(dir, "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\ncat "+fixture+"\n"), 0o755)
	res, err := claudeRunner{}.Run(context.Background(), RunRequest{Bin: bin, WorkDir: dir, Prompt: "hi"}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Limits == nil || res.Limits.Status != "allowed" || len(res.Limits.Windows) != 3 {
		t.Fatalf("limits = %+v", res.Limits)
	}
	w := res.Limits.Windows["five_hour"]
	if w.Utilization != 0.19 || !w.ResetsAt.Equal(time.Unix(1790593200, 0)) {
		t.Fatalf("five_hour = %+v", w)
	}
	// context = the last call's input (with cache), in the main model's window
	if res.Context.Tokens != 31012 || res.Context.Window != 1000000 {
		t.Fatalf("context = %+v", res.Context)
	}
}
