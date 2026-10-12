package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// CleanConversation and CleanTask run several DELETE/UPDATE statements; if one
// fails partway, none of it must stick (ADR-095 cleanup must be atomic).

func TestCleanConversationAtomicOnFailure(t *testing.T) {
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
	if _, err := s.Chat().AddMessage(ctx, storage.Message{ConversationID: c.ID, Role: "user", Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Chat().AddPatch(ctx, storage.Patch{ConversationID: c.ID, Diff: "d"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Chat().UpsertMember(ctx, storage.ChatMember{ConversationID: c.ID, AgentID: "agt_1", SessionID: "sess_1"}); err != nil {
		t.Fatal(err)
	}

	// Force the final UPDATE on conversations to fail, after the DELETEs and
	// the INSERT have already run.
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER boom_conv BEFORE UPDATE ON conversations WHEN NEW.cleaned <> '' BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}

	if err := s.Chat().CleanConversation(ctx, c.ID, "content", "cleaned"); err == nil {
		t.Fatal("expected the forced failure to surface")
	}

	msgs, err := s.Chat().ListMessages(ctx, c.ID)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("messages after a failed clean = %v, %v (want the original untouched)", msgs, err)
	}
	patches, err := s.Chat().ListPatches(ctx, c.ID)
	if err != nil || len(patches) != 1 {
		t.Fatalf("patches after a failed clean = %v, %v (want the original untouched)", patches, err)
	}
	members, err := s.Chat().Members(ctx, c.ID)
	if err != nil || len(members) != 1 || members[0].SessionID != "sess_1" {
		t.Fatalf("members after a failed clean = %+v, %v (want the session kept)", members, err)
	}
	got, err := s.Chat().GetConversation(ctx, c.ID)
	if err != nil || got.Cleaned != "" {
		t.Fatalf("conversation after a failed clean = %+v, %v (want cleaned to stay empty)", got, err)
	}
}

func TestCleanTaskAtomicOnFailure(t *testing.T) {
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
	tk, err := s.Tasks().Create(ctx, storage.Task{ProjectID: p.ID, Goal: "g", Status: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Tasks().AddStep(ctx, storage.TaskStep{TaskID: tk.ID, Seq: 1, Phase: "work", Instruction: "i", Output: "o", Status: "done"}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER boom_task BEFORE UPDATE ON tasks WHEN NEW.cleaned <> '' BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}

	if err := s.Tasks().CleanTask(ctx, tk.ID, "content", "cleaned"); err == nil {
		t.Fatal("expected the forced failure to surface")
	}

	steps, err := s.Tasks().ListSteps(ctx, tk.ID)
	if err != nil || len(steps) != 1 {
		t.Fatalf("steps after a failed clean = %v, %v (want the original untouched)", steps, err)
	}
	got, err := s.Tasks().Get(ctx, tk.ID)
	if err != nil || got.Cleaned != "" {
		t.Fatalf("task after a failed clean = %+v, %v (want cleaned to stay empty)", got, err)
	}
}
