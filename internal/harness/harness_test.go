package harness_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/harness"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
)

// The bundle: assets from the folder; diffs split by who ended them; a merged
// diff counts as checked only with a check since the chat's previous one;
// office's own messages are not the person correcting an agent.
func TestCollect(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# rules\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".github/workflows"), 0o755)
	os.WriteFile(filepath.Join(dir, ".github/workflows/ci.yml"), []byte("on: push\n"), 0o644)
	p, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: dir})
	c, err := st.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: p.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	msg := func(role, author, content string, tools ...storage.ToolCall) {
		if _, err := st.Chat().AddMessage(ctx, storage.Message{ConversationID: c.ID, Role: role, Author: author, Content: content, Tools: tools}); err != nil {
			t.Fatal(err)
		}
	}
	patch := func(status, by, detail string) {
		if _, err := st.Chat().AddPatch(ctx, storage.Patch{ConversationID: c.ID, Diff: "d", Status: status, DecidedBy: by, Detail: detail}); err != nil {
			t.Fatal(err)
		}
	}
	msg("assistant", "", "ok", storage.ToolCall{Name: "Bash", Summary: "Chạy go test ./internal/..."})
	patch("applied", "human:a@x.io", "")
	patch("applied", "human:a@x.io", "") // nothing ran since the previous one
	patch("rejected", "human:a@x.io", "không đúng ý")
	patch("rejected", "office", "Thay bằng thay đổi mới hơn")
	patch("failed", "", "Chưa gộp được: còn dấu xung đột ở a.go")
	msg("user", "human:a@x.io", "sai rồi, làm lại đi")
	msg("user", "human:a@x.io", "[office] a@x.io decided your proposals: sai")
	msg("user", "burn:a@x.io", "vẫn lỗi")
	st.Actions().Create(ctx, storage.Action{ProjectID: p.ID, ConversationID: c.ID, Kind: "run_command", Target: "rm -rf x", Status: "rejected"})

	ev, err := harness.Collect(ctx, st, p, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Assets.Instructions) != 1 || len(ev.Assets.CI) != 1 {
		t.Fatalf("assets = %+v", ev.Assets)
	}
	d := ev.Diffs
	if d.Total != 5 || d.Merged != 2 || d.RejectedByPerson != 1 || d.Superseded != 1 || d.FailedToMerge != 1 || d.Rejected[0].Text != "không đúng ý" {
		t.Fatalf("diffs = %+v", d)
	}
	if v := ev.Validation; v.MergedDiffs != 2 || v.WithCheck != 1 || len(v.WithoutCheck) != 1 || v.CheckCommands[0].Key != "go test" {
		t.Fatalf("validation = %+v", v)
	}
	if co := ev.Corrections; co.Messages != 1 || co.Count != 1 {
		t.Fatalf("corrections = %+v", co)
	}
	if r := ev.Commands.Refused; len(r) != 1 || r[0].Key != "run_command: rm -rf x" {
		t.Fatalf("refused = %+v", r)
	}
}
