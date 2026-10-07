package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAntigravityRunner(t *testing.T) {
	tmp := t.TempDir()
	bin, log := filepath.Join(tmp, "agy"), filepath.Join(tmp, "log")
	os.WriteFile(bin, []byte(`#!/bin/sh
in=$(cat)
echo "ARGS $*" >> `+log+`
echo "STDIN $in" >> `+log+`
case "$*" in *"--conversation gone"*)
  echo '{"event":"result","result":{"conversation_id":"","status":"ERROR","response":"","error":"conversation not found"}}'; exit 1;; esac
echo '{"event":"init","init":{"cwd":"/w","tools":[],"permission_mode":"default","model":"gemini-3.8-flash-high"}}'
echo '{"event":"step_update","step_update":{"conversation_id":"c1","step_index":0,"state":"DONE","step_type":"user_input"}}'
echo '{"event":"step_update","step_update":{"conversation_id":"c1","step_index":1,"state":"ACTIVE","step_type":"agent_response","text_delta":"Xin "}}'
echo '{"event":"step_update","step_update":{"conversation_id":"c1","step_index":2,"state":"ACTIVE","step_type":"tool","tool_name":"read_file","tool_info":{"file_path":"a.go"}}}'
echo '{"event":"step_update","step_update":{"conversation_id":"c1","step_index":2,"state":"DONE","step_type":"tool","tool_name":"read_file"}}'
echo '{"event":"step_update","step_update":{"conversation_id":"c1","step_index":3,"state":"ACTIVE","step_type":"agent_response","text_delta":"chào"}}'
echo '{"event":"result","result":{"conversation_id":"c1","status":"SUCCESS","response":"Xin chào","num_turns":1,"usage":{"input_tokens":10,"output_tokens":4,"thinking_tokens":2,"cache_read_tokens":5}}}'
`), 0o755)
	hist := []HistoryItem{{Role: "user", Content: "câu cũ"}, {Role: "assistant", Content: "trả lời cũ"}}
	req := RunRequest{Bin: bin, System: "HƯỚNG DẪN", History: hist, Prompt: "mới", WorkDir: tmp, Effort: "high", ExtraDirs: []string{"/x"}}
	var text strings.Builder
	res, err := antigravityRunner{}.Run(context.Background(), req, func(e Event) {
		if e.Type == "text" {
			text.WriteString(e.Text)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if text.String() != "Xin chào" || res.Text != "Xin chào" || res.SessionID != "c1" || res.Usage.Model != "gemini-3.8-flash-high" ||
		res.Usage.InputTokens != 15 || res.Usage.OutputTokens != 6 {
		t.Fatalf("text=%q res=%+v", text.String(), res)
	}
	if len(res.Tools) != 1 || res.Tools[0].Summary != "Đọc a.go" {
		t.Fatalf("tools = %+v", res.Tools)
	}
	raw, _ := os.ReadFile(log)
	got := string(raw)
	for _, want := range []string{"--mode plan", "--effort high", "--add-dir /x", "HƯỚNG DẪN", "câu cũ"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}

	// going on in the conversation, allowed to edit: only the new message
	os.Remove(log)
	req.SessionID, req.Write = "c1", true
	if _, err := (antigravityRunner{}).Run(context.Background(), req, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(log)
	if got = string(raw); !strings.Contains(got, "--conversation c1") || !strings.Contains(got, "--mode accept-edits") || strings.Contains(got, "HƯỚNG DẪN") {
		t.Fatalf("resume call:\n%s", got)
	}

	// a conversation agy lost: start over with the transcript
	os.Remove(log)
	req.SessionID = "gone"
	if res, err = (antigravityRunner{}).Run(context.Background(), req, func(Event) {}); err != nil || res.SessionID != "c1" {
		t.Fatalf("lost: %+v %v", res, err)
	}
	raw, _ = os.ReadFile(log)
	if got = string(raw); strings.Count(got, "ARGS") != 2 || !strings.Contains(got, "HƯỚNG DẪN") {
		t.Fatalf("fallback calls:\n%s", got)
	}
}

func TestAntigravityRunnerSignedOut(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "agy")
	os.WriteFile(bin, []byte(`#!/bin/sh
cat >/dev/null
echo "Error: authentication required. Run 'agy' to log in, then retry." >&2
echo '{"event":"result","result":{"conversation_id":"","status":"ERROR","response":"","error":"authentication failed or timed out"}}'
`), 0o755)
	_, err := antigravityRunner{}.Run(context.Background(), RunRequest{Bin: bin, Prompt: "x", WorkDir: tmp}, func(Event) {})
	if err == nil || !strings.Contains(err.Error(), "chưa đăng nhập") {
		t.Fatalf("err = %v", err)
	}
}
