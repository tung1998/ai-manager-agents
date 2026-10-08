package chat_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Written while the chat answers, messages wait in office, not the page; once
// it is free they go together as the next message, as their author.
func TestQueuedGoTogetherOnceFree(t *testing.T) {
	g := newGroup(t)
	ctx := actor.With(g.context, "human:a@x.io")
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	turn, _, err := g.engine.Send(ctx, g.conv.ID, "làm X", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"thêm Y", "và Z"} {
		if _, err := g.engine.Queue(ctx, g.conv.ID, text, "", nil, storage.QueuedOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if q, _ := g.f.st.Chat().QueuedMessages(ctx, g.conv.ID); len(q) != 2 {
		t.Fatalf("waiting = %d, want 2", len(q))
	}
	os.Remove(filepath.Join(g.dir, "sleep-lead"))
	collect(t, turn)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(30 * time.Millisecond) {
		msgs, _ := g.f.st.Chat().ListMessages(ctx, g.conv.ID)
		var users []storage.Message
		for _, m := range msgs {
			if m.Role == "user" {
				users = append(users, m)
			}
		}
		if len(users) == 2 {
			if m := users[1]; m.Content != "thêm Y\n\nvà Z" || m.Author != "human:a@x.io" {
				t.Fatalf("sent = %q by %s", m.Content, m.Author)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the waiting messages were not sent: %d user messages", len(users))
		}
	}
	if q, _ := g.f.st.Chat().QueuedMessages(ctx, g.conv.ID); len(q) != 0 {
		t.Fatalf("still waiting: %d", len(q))
	}
}

// A free chat sends what is queued at once; taken back, nothing goes.
func TestQueuedFreeOrTakenBack(t *testing.T) {
	g := newGroup(t)
	ctx := actor.With(g.context, "human:a@x.io")
	if _, err := g.engine.Queue(ctx, g.conv.ID, "ngay đi", "", nil, storage.QueuedOptions{}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(30 * time.Millisecond) {
		msgs, _ := g.f.st.Chat().ListMessages(ctx, g.conv.ID)
		if len(msgs) > 0 && strings.Contains(msgs[0].Content, "ngay đi") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a free chat kept it waiting")
		}
	}
	if tr, ok := g.engine.Active(g.conv.ID); ok {
		collect(t, tr)
	}
	os.WriteFile(filepath.Join(g.dir, "sleep-lead"), []byte("1"), 0o644)
	turn, _, err := g.engine.Send(ctx, g.conv.ID, "lần hai", nil)
	if err != nil {
		t.Fatal(err)
	}
	g.engine.Queue(ctx, g.conv.ID, "bỏ", "", nil, storage.QueuedOptions{})
	g.f.st.Chat().DeleteQueued(ctx, g.conv.ID)
	collect(t, turn)
	time.Sleep(300 * time.Millisecond)
	msgs, _ := g.f.st.Chat().ListMessages(ctx, g.conv.ID)
	for _, m := range msgs {
		if m.Content == "bỏ" {
			t.Fatal("a message taken back was sent")
		}
	}
}
