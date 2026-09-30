package sqlite_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// Every write says which table it changed (the dashboard's live updates);
// reads say nothing; a transaction's writes too.
func TestOnWrite(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	var mu sync.Mutex
	var got []string
	st.(*sqlite.Store).OnWrite(func(table string) { mu.Lock(); got = append(got, table); mu.Unlock() })
	c, _ := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p.ID, Title: "x"})
	st.Chat().ListConversations(ctx, p.ID, 10)
	st.InTx(ctx, func(tx storage.Store) error {
		_, err := tx.Jobs().Create(ctx, storage.Job{ProjectID: p.ID, Kind: "chat_turn", Origin: "user", ConversationID: c.ID, Status: "pending"})
		return err
	})
	st.Jobs().Claim(ctx, time.Now().UTC(), 5, nil)
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(got, "conversations") || !slices.Contains(got, "jobs") {
		t.Fatalf("tables = %v", got)
	}
	for _, x := range got {
		if x == "" {
			t.Fatalf("an unnamed write: %v", got)
		}
	}
}

// With live updates on, a replace is still one transaction: a failure leaves
// the notes as they were, and says nothing changed.
func TestOnWriteKeepsTransactions(t *testing.T) {
	ctx := context.Background()
	st, p := openStore(t)
	mem := st.Memories()
	mem.Create(ctx, storage.Memory{ProjectID: p.ID, AgentID: "agt_1", Text: "cũ", Source: "person"})
	var got []string
	st.(*sqlite.Store).OnWrite(func(table string) { got = append(got, table) })
	dup := storage.Memory{ID: "mem_same", Text: "a"}
	if err := st.Memories().Replace(ctx, p.ID, "agt_1", []storage.Memory{dup, dup}); err == nil {
		t.Fatal("a duplicate id was stored")
	}
	if list, _ := st.Memories().List(ctx, p.ID, "agt_1"); len(list) != 1 || list[0].Text != "cũ" {
		t.Fatalf("a failed replace changed the notes: %+v", list)
	}
	if len(got) != 0 {
		t.Fatalf("a rolled back write was told: %v", got)
	}
	if err := st.Memories().Replace(ctx, p.ID, "agt_1", []storage.Memory{{Text: "mới"}}); err != nil || !slices.Contains(got, "agent_memories") {
		t.Fatalf("replace = %v, told %v", err, got)
	}
}
