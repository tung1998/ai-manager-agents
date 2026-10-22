package chat_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// Office going down cuts an answer off without ending its job; the next
// office goes on with it in the agent's session.
func TestRestartGoesOn(t *testing.T) {
	tmp := t.TempDir()
	bin, argsLog := filepath.Join(tmp, "claude"), filepath.Join(tmp, "args")
	os.WriteFile(bin, []byte(`#!/bin/sh
echo "$*" >> `+argsLog+`
cat >/dev/null
echo '{"type":"system","subtype":"init","session_id":"sess-1"}'
case "$*" in
*"--resume sess-1"*) echo '{"type":"result","subtype":"success","is_error":false,"result":"xong phần còn lại","session_id":"sess-1","usage":{"input_tokens":1,"output_tokens":1}}' ;;
*) sleep 30 ;;
esac
`), 0o755)
	f := setup(t, func(provs *provider.Service) storage.Provider {
		p, err := provs.Create(context.Background(), provider.Input{Name: "CC", Kind: storage.ProviderClaudeCLI, BaseURL: bin})
		if err != nil {
			t.Fatal(err)
		}
		return p
	})
	ctx := actor.With(context.Background(), "human:a@b.c")
	conv, _ := f.engine.StartConversation(ctx, f.project.ID, "")
	first, _, err := f.engine.Send(ctx, conv.ID, "làm việc dài", nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second) // it started and said its session
	start := time.Now()
	f.engine.Shutdown(5 * time.Second)
	if evs := collect(t, first); evs[len(evs)-1].Type != "error" { // the stream watching it ends
		t.Fatalf("cut turn: %+v", evs[len(evs)-1])
	}
	if f.engine.ActiveTurns() != 0 || time.Since(start) > 4*time.Second {
		t.Fatalf("not ended at once: %d turns, %s", f.engine.ActiveTurns(), time.Since(start))
	}
	if _, _, err := f.engine.Send(ctx, conv.ID, "thêm", nil); err != chat.ErrStopping {
		t.Fatalf("a message while going down: %v", err)
	}
	jobs, _ := f.st.Jobs().List(ctx, storage.JobFilter{Kind: "chat_turn", Status: "running"})
	msgs, _ := f.st.Chat().ListMessages(ctx, conv.ID)
	members, _ := f.st.Chat().Members(ctx, conv.ID)
	if len(jobs) != 1 || len(msgs) != 1 || len(members) != 1 || members[0].SessionID != "sess-1" {
		t.Fatalf("jobs=%d msgs=%+v members=%+v", len(jobs), msgs, members)
	}

	// the next office
	next := chat.NewEngine(f.st, f.provs, usage.New(f.st, time.UTC))
	if n := next.ResumeInterrupted(context.Background(), jobs); n != 1 {
		t.Fatalf("went on in %d chats", n)
	}
	turn, ok := next.Active(conv.ID)
	if !ok {
		t.Fatal("no turn")
	}
	events := collect(t, turn)
	if done := events[len(events)-1]; done.Type != "done" || done.Message.Content != "xong phần còn lại" {
		t.Fatalf("last event: %+v", done)
	}
	b, _ := os.ReadFile(argsLog)
	if calls := strings.Split(string(b), "-p --output-format"); len(calls) != 3 || strings.Contains(calls[1], "--resume") || !strings.Contains(calls[2], "--resume sess-1") {
		t.Fatalf("calls: %q", calls)
	}
	msgs, _ = f.st.Chat().ListMessages(ctx, conv.ID)
	if note := msgs[1]; note.Role != "user" || !strings.HasPrefix(note.Content, "@"+members[0].AgentName) || note.Author != "human:a@b.c" {
		t.Fatalf("note: %+v", note)
	}
}
