package chat_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/chat"
)

// fakeClaude writes a claude CLI that logs each call to log; ok answers
// "từ <name>", otherwise it fails like a hit limit before doing anything.
func fakeClaude(t *testing.T, name string, ok bool) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	bin, log = filepath.Join(dir, "claude"), filepath.Join(dir, "calls")
	out := `echo '{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Claude AI usage limit reached","usage":{"input_tokens":0,"output_tokens":0}}'`
	if ok {
		out = `echo '{"type":"result","subtype":"success","is_error":false,"result":"từ ` + name + `","usage":{"input_tokens":3,"output_tokens":2}}'`
	}
	os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in auth*) echo '{"loggedIn":true}'; exit 0;; esac
cat >/dev/null
echo x >> `+log+`
`+out+`
`), 0o755)
	return bin, log
}

func calls(log string) int {
	b, _ := os.ReadFile(log)
	return strings.Count(string(b), "x")
}

func answer(t *testing.T, f fixture) (string, []chat.Event) {
	t.Helper()
	ctx := context.Background()
	conv, _ := f.engine.StartConversation(ctx, f.project.ID, "")
	turn, _, err := f.engine.Send(ctx, conv.ID, "chào", nil)
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, turn)
	last := events[len(events)-1]
	if last.Type != "done" {
		t.Fatalf("last event = %+v", last)
	}
	return last.Message.Content, events
}
