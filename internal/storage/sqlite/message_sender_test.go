package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A message written to a bot keeps who wrote it; one written in office has none.
func TestMessageSender(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := s.Repos().Create(ctx, storage.Repo{Name: "p", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	from := storage.Sender{Kind: "discord", ID: "7", Username: "an", Name: "An Nguyễn", Private: true, Link: "https://discord.com/channels/@me/c1/m1"}
	if _, err := s.Chat().AddMessage(ctx, storage.Message{ConversationID: c.ID, Role: "user", Content: "hi", Sender: &from}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Chat().AddMessage(ctx, storage.Message{ConversationID: c.ID, Role: "user", Content: "mine"}); err != nil {
		t.Fatal(err)
	}
	msgs, err := s.Chat().ListMessages(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Sender == nil || *msgs[0].Sender != from || msgs[1].Sender != nil {
		t.Fatalf("msgs = %+v", msgs)
	}
}
