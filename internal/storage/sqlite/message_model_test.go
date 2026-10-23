package sqlite_test

import (
	"context"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A message comes back with the connection and model of the run that wrote it.
func TestListMessagesCarriesRunModel(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	c, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p.ID})
	r, err := st.Runs().Create(ctx, storage.Run{Kind: "chat", ProjectID: p.ID, ProviderName: "Claude Max", Model: "claude-opus-5-5", Status: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	st.Chat().AddMessage(ctx, storage.Message{ConversationID: c.ID, Role: "user", Content: "hi"})
	st.Chat().AddMessage(ctx, storage.Message{ConversationID: c.ID, Role: "assistant", Content: "hello", RunID: r.ID})
	msgs, err := st.Chat().ListMessages(ctx, c.ID)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("list = %+v %v", msgs, err)
	}
	if msgs[0].Provider != "" || msgs[0].Model != "" {
		t.Fatalf("user message has a model: %+v", msgs[0])
	}
	if msgs[1].Provider != "Claude Max" || msgs[1].Model != "claude-opus-5-5" {
		t.Fatalf("assistant message = %q %q", msgs[1].Provider, msgs[1].Model)
	}
}
