package chat_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// A chat's proposals are approved in the order its agent made them: a later
// one waits for the earlier ones; rejecting never waits.
func TestCheckOrder(t *testing.T) {
	g := newGroup(t)
	ctx := g.context
	first, _ := g.f.st.Actions().Create(ctx, storage.Action{ProjectID: g.f.project.ID, ConversationID: g.conv.ID, Kind: "run_command", Target: "git add -A"})
	time.Sleep(2 * time.Millisecond)
	patch, _ := g.f.st.Chat().AddPatch(ctx, storage.Patch{ConversationID: g.conv.ID, Diff: "x", Files: []string{"a.go", "b.go"}})
	time.Sleep(2 * time.Millisecond)
	last, _ := g.f.st.Actions().Create(ctx, storage.Action{ProjectID: g.f.project.ID, ConversationID: g.conv.ID, Kind: "run_command", Target: "git status"})

	if err := g.engine.CheckOrder(ctx, g.conv.ID, first.ID); err != nil {
		t.Fatalf("the oldest one is free: %v", err)
	}
	var oe *chat.OrderError
	if err := g.engine.CheckOrder(ctx, g.conv.ID, last.ID); !errors.As(err, &oe) || oe.Earlier == "" {
		t.Fatalf("a later one waits for the earlier: %v", err)
	}
	first.Status = "rejected"
	_ = g.f.st.Actions().Update(ctx, first)
	if err := g.engine.CheckOrder(ctx, g.conv.ID, patch.ID); err != nil {
		t.Fatalf("the diff is now the oldest: %v", err)
	}
	if err := g.engine.CheckOrder(ctx, g.conv.ID, last.ID); !errors.As(err, &oe) || oe.Earlier != "diff 2 file" {
		t.Fatalf("the diff still waits before it: %v", err)
	}
	if !g.engine.Waiting(ctx, g.conv.ID) {
		t.Fatal("two proposals still wait")
	}
}

// The agent goes on only once every proposal of the chat is decided: one
// decided while another waits holds its word until the last is (Settled for
// a skipped one).
func TestDecidedWaitsForAllProposals(t *testing.T) {
	g := newGroup(t)
	g.sendAll(t, "chuẩn bị deploy")
	ctx := g.context
	other, _ := g.f.st.Actions().Create(ctx, storage.Action{ProjectID: g.f.project.ID, ConversationID: g.conv.ID, Kind: "run_command", Target: "pnpm build"})
	g.engine.SetDecidedWait(50 * time.Millisecond)
	g.engine.Decided(g.conv.ID, "admin@x.io", "✅ Đã duyệt Chạy lệnh: pnpm test — ok")
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(g.dir, "call2.args")); err == nil {
		t.Fatal("the agent went on while a proposal still waits")
	}
	other.Status = "rejected" // skipped: no word of its own
	_ = g.f.st.Actions().Update(ctx, other)
	g.engine.Settled(g.conv.ID)
	g.waitAuthors(t, 2)
	if _, in := call(t, g.dir, 2); !strings.Contains(in, "pnpm test") {
		t.Fatalf("the held decision goes on:\n%s", in)
	}
}
